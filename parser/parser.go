package parser

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"github.com/bnulwh/ollama/api"
)

var ErrModelNotFound = errors.New("no Modelfile or safetensors files found")

// Modelfile 表示解析后的模型文件结构
type Modelfile struct {
	Commands []Command // 存储所有解析后的命令
}

func (f Modelfile) String() string {
	var sb strings.Builder
	for _, cmd := range f.Commands {
		fmt.Fprintln(&sb, cmd.String())
	}

	return sb.String()
}

var deprecatedParameters = []string{"penalize_newline"}

// 将 Modelfile 转换为 API 请求结构体
// CreateRequest creates a new *api.CreateRequest from an existing Modelfile
func (f Modelfile) CreateRequest(relativeDir string) (*api.CreateRequest, error) {
	req := &api.CreateRequest{} // 初始化请求

	var messages []api.Message
	var licenses []string
	params := make(map[string]any)
	// 遍历所有命令，分类处理
	for _, c := range f.Commands {
		switch c.Name {
		case "model":
			// 处理模型路径，
			path, err := expandPath(c.Args, relativeDir)
			if err != nil {
				return nil, err
			}
			//计算文件哈希
			digestMap, err := fileDigestMap(path)
			if errors.Is(err, os.ErrNotExist) {
				req.From = c.Args
				continue
			} else if err != nil {
				return nil, err
			}

			if req.Files == nil {
				req.Files = digestMap
			} else {
				for k, v := range digestMap {
					req.Files[k] = v
				}
			}
		case "adapter":
			// 处理适配器文件
			path, err := expandPath(c.Args, relativeDir)
			if err != nil {
				return nil, err
			}

			digestMap, err := fileDigestMap(path)
			if err != nil {
				return nil, err
			}

			req.Adapters = digestMap
		case "template":
			// 设置模板
			req.Template = c.Args
		case "system":
			// 设置系统消息
			req.System = c.Args
		case "license":
			licenses = append(licenses, c.Args)
		case "message":
			// 解析消息角色和内容（如 "user: Hello"）
			role, msg, _ := strings.Cut(c.Args, ": ")
			messages = append(messages, api.Message{Role: role, Content: msg})
		default:
			// 处理参数（如 "learning_rate 0.01"）
			if slices.Contains(deprecatedParameters, c.Name) {
				fmt.Printf("warning: parameter %s is deprecated\n", c.Name)
				break
			}

			ps, err := api.FormatParams(map[string][]string{c.Name: {c.Args}})
			if err != nil {
				return nil, err
			}

			for k, v := range ps {
				if ks, ok := params[k].([]string); ok {
					params[k] = append(ks, v.([]string)...)
				} else if vs, ok := v.([]string); ok {
					params[k] = vs
				} else {
					params[k] = v
				}
			}
		}
	}

	if len(params) > 0 {
		req.Parameters = params
	}
	if len(messages) > 0 {
		req.Messages = messages
	}
	if len(licenses) > 0 {
		req.License = licenses
	}

	return req, nil
}

// 遍历目录或单个文件，生成哈希映射
func fileDigestMap(path string) (map[string]string, error) {
	fl := make(map[string]string)

	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	var files []string
	if fi.IsDir() {
		files, err = filesForModel(path) // 匹配模型文件模式（如 *.safetensors）
		if err != nil {
			return nil, err
		}
	} else {
		files = []string{path}
	}

	for _, f := range files {
		digest, err := digestForFile(f)
		if err != nil {
			return nil, err
		}
		fl[f] = digest
	}

	return fl, nil
}

// 计算文件的 SHA256 哈希
func digestForFile(filename string) (string, error) {
	filepath, err := filepath.EvalSymlinks(filename)
	if err != nil {
		return "", err
	}

	bin, err := os.Open(filepath)
	if err != nil {
		return "", err
	}
	defer bin.Close()

	hash := sha256.New()
	// 读取文件内容并计算哈希
	if _, err := io.Copy(hash, bin); err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}

// 根据内容获取文件类型
func detectContentType(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var b bytes.Buffer
	b.Grow(512)

	if _, err := io.CopyN(&b, f, 512); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	contentType, _, _ := strings.Cut(http.DetectContentType(b.Bytes()), ";")
	return contentType, nil
}

// 双重匹配文件，包括扩展名和文件类型
func glob(pattern, contentType string) ([]string, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	for _, safetensor := range matches {
		if ct, err := detectContentType(safetensor); err != nil {
			return nil, err
		} else if ct != contentType {
			return nil, fmt.Errorf("invalid content type: expected %s for %s", ct, safetensor)
		}
	}

	return matches, nil
}

// 匹配模型文件模式（支持 safetensors、bin、gguf 等）
func filesForModel(path string) ([]string, error) {
	// 文件内容校验
	// detectContentType :=

	// 使用 glob 模式匹配文件
	var files []string
	if st, _ := glob(filepath.Join(path, "model*.safetensors"), "application/octet-stream"); len(st) > 0 {
		// safetensors files might be unresolved git lfs references; skip if they are
		// covers model-x-of-y.safetensors, model.fp32-x-of-y.safetensors, model.safetensors
		files = append(files, st...)
	} else if st, _ := glob(filepath.Join(path, "adapters.safetensors"), "application/octet-stream"); len(st) > 0 {
		// covers adapters.safetensors
		files = append(files, st...)
	} else if st, _ := glob(filepath.Join(path, "adapter_model.safetensors"), "application/octet-stream"); len(st) > 0 {
		// covers adapter_model.safetensors
		files = append(files, st...)
	} else if pt, _ := glob(filepath.Join(path, "pytorch_model*.bin"), "application/zip"); len(pt) > 0 {
		// pytorch files might also be unresolved git lfs references; skip if they are
		// covers pytorch_model-x-of-y.bin, pytorch_model.fp32-x-of-y.bin, pytorch_model.bin
		files = append(files, pt...)
	} else if pt, _ := glob(filepath.Join(path, "consolidated*.pth"), "application/zip"); len(pt) > 0 {
		// pytorch files might also be unresolved git lfs references; skip if they are
		// covers consolidated.x.pth, consolidated.pth
		files = append(files, pt...)
	} else if gg, _ := glob(filepath.Join(path, "*.gguf"), "application/octet-stream"); len(gg) > 0 {
		// covers gguf files ending in .gguf
		files = append(files, gg...)
	} else if gg, _ := glob(filepath.Join(path, "*.bin"), "application/octet-stream"); len(gg) > 0 {
		// covers gguf files ending in .bin
		files = append(files, gg...)
	} else {
		return nil, ErrModelNotFound
	}

	// add configuration files, json files are detected as text/plain
	js, err := glob(filepath.Join(path, "*.json"), "text/plain")
	if err != nil {
		return nil, err
	}
	files = append(files, js...)

	// bert models require a nested config.json
	// TODO(mxyng): merge this with the glob above
	js, err = glob(filepath.Join(path, "**/*.json"), "text/plain")
	if err != nil {
		return nil, err
	}
	files = append(files, js...)

	if tks, _ := glob(filepath.Join(path, "tokenizer.model"), "application/octet-stream"); len(tks) > 0 {
		// add tokenizer.model if it exists, tokenizer.json is automatically picked up by the previous glob
		// tokenizer.model might be a unresolved git lfs reference; error if it is
		files = append(files, tks...)
	} else if tks, _ := glob(filepath.Join(path, "**/tokenizer.model"), "text/plain"); len(tks) > 0 {
		// some times tokenizer.model is in a subdirectory (e.g. meta-llama/Meta-Llama-3-8B)
		files = append(files, tks...)
	}

	return files, nil
}

// Command 表示单条命令（如 FROM、PARAMETER）
type Command struct {
	Name string // 命令名称（如 "model"）
	Args string // 命令参数（如文件路径或配置值）
}

func (c Command) String() string {
	var sb strings.Builder
	switch c.Name {
	case "model":
		fmt.Fprintf(&sb, "FROM %s", c.Args)
	case "license", "template", "system", "adapter":
		fmt.Fprintf(&sb, "%s %s", strings.ToUpper(c.Name), quote(c.Args))
	case "message":
		role, message, _ := strings.Cut(c.Args, ": ")
		fmt.Fprintf(&sb, "MESSAGE %s %s", role, quote(message))
	default:
		fmt.Fprintf(&sb, "PARAMETER %s %s", c.Name, quote(c.Args))
	}

	return sb.String()
}

// 状态定义（解析过程中的不同阶段）
type state int

const (
	stateNil   state = iota
	stateName        // 解析命令名称
	stateValue       // 解析命令参数
	stateParameter
	stateMessage
	stateComment // 解析注释
)

var (
	errMissingFrom        = errors.New("no FROM line")
	errInvalidMessageRole = errors.New("message role must be one of \"system\", \"user\", or \"assistant\"")
	errInvalidCommand     = errors.New("command must be one of \"from\", \"license\", \"template\", \"system\", \"adapter\", \"parameter\", or \"message\"")
)

// 自定义解析错误（包含行号）
type ParserError struct {
	LineNumber int
	Msg        string
}

func (e *ParserError) Error() string {
	if e.LineNumber > 0 {
		return fmt.Sprintf("(line %d): %s", e.LineNumber, e.Msg)
	}
	return e.Msg
}

// 解析 Modelfile 内容
func ParseFile(r io.Reader) (*Modelfile, error) {
	var cmd Command
	var curr state
	var currLine int = 1
	var b bytes.Buffer
	var role string

	var f Modelfile
	// 处理 BOM 标记（兼容 UTF-8 编码）
	tr := unicode.BOMOverride(unicode.UTF8.NewDecoder())
	br := bufio.NewReader(transform.NewReader(r, tr))
	// 逐字符解析
	for {
		r, _, err := br.ReadRune()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}

		if isNewline(r) {
			currLine++
		}

		next, r, err := parseRuneForState(r, curr)
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("%w: %s", err, b.String())
		} else if err != nil {
			return nil, &ParserError{
				LineNumber: currLine,
				Msg:        err.Error(),
			}
		}

		// process the state transition, some transitions need to be intercepted and redirected
		if next != curr {
			switch curr {
			case stateName:
				if !isValidCommand(b.String()) {
					return nil, &ParserError{
						LineNumber: currLine,
						Msg:        errInvalidCommand.Error(),
					}
				}

				// next state sometimes depends on the current buffer value
				switch s := strings.ToLower(b.String()); s {
				case "from":
					cmd.Name = "model"
				case "parameter":
					// transition to stateParameter which sets command name
					next = stateParameter
				case "message":
					// transition to stateMessage which validates the message role
					next = stateMessage
					fallthrough
				default:
					cmd.Name = s
				}
			case stateParameter:
				cmd.Name = b.String()
			case stateMessage:
				if !isValidMessageRole(b.String()) {
					return nil, &ParserError{
						LineNumber: currLine,
						Msg:        errInvalidMessageRole.Error(),
					}
				}

				role = b.String()
			case stateComment, stateNil:
				// pass
			case stateValue:
				s, ok := unquote(strings.TrimSpace(b.String()))
				if !ok || isSpace(r) {
					if _, err := b.WriteRune(r); err != nil {
						return nil, err
					}

					continue
				}

				if role != "" {
					s = role + ": " + s
					role = ""
				}

				cmd.Args = s
				f.Commands = append(f.Commands, cmd)
			}

			b.Reset()
			curr = next
		}

		if strconv.IsPrint(r) {
			if _, err := b.WriteRune(r); err != nil {
				return nil, err
			}
		}
	}

	// flush the buffer
	switch curr {
	case stateComment, stateNil:
		// pass; nothing to flush
	case stateValue:
		s, ok := unquote(strings.TrimSpace(b.String()))
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}

		if role != "" {
			s = role + ": " + s
		}

		cmd.Args = s
		f.Commands = append(f.Commands, cmd)
	default:
		return nil, io.ErrUnexpectedEOF
	}

	for _, cmd := range f.Commands {
		if cmd.Name == "model" {
			return &f, nil
		}
	}

	return nil, errMissingFrom
}

func parseRuneForState(r rune, cs state) (state, rune, error) {
	switch cs {
	case stateNil:
		switch {
		case r == '#':
			return stateComment, 0, nil
		case isSpace(r), isNewline(r):
			return stateNil, 0, nil
		default:
			return stateName, r, nil
		}
	case stateName:
		switch {
		case isAlpha(r):
			return stateName, r, nil
		case isSpace(r):
			return stateValue, 0, nil
		default:
			return stateNil, 0, errInvalidCommand
		}
	case stateValue:
		switch {
		case isNewline(r):
			return stateNil, r, nil
		case isSpace(r):
			return stateNil, r, nil
		default:
			return stateValue, r, nil
		}
	case stateParameter:
		switch {
		case isAlpha(r), isNumber(r), r == '_':
			return stateParameter, r, nil
		case isSpace(r):
			return stateValue, 0, nil
		default:
			return stateNil, 0, io.ErrUnexpectedEOF
		}
	case stateMessage:
		switch {
		case isAlpha(r):
			return stateMessage, r, nil
		case isSpace(r):
			return stateValue, 0, nil
		default:
			return stateNil, 0, io.ErrUnexpectedEOF
		}
	case stateComment:
		switch {
		case isNewline(r):
			return stateNil, 0, nil
		default:
			return stateComment, 0, nil
		}
	default:
		return stateNil, 0, errors.New("")
	}
}

func quote(s string) string {
	if strings.Contains(s, "\n") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		if strings.Contains(s, "\"") {
			return `"""` + s + `"""`
		}

		return `"` + s + `"`
	}

	return s
}

func unquote(s string) (string, bool) {
	// TODO: single quotes
	if len(s) >= 3 && s[:3] == `"""` {
		if len(s) >= 6 && s[len(s)-3:] == `"""` {
			return s[3 : len(s)-3], true
		}

		return "", false
	}

	if len(s) >= 1 && s[0] == '"' {
		if len(s) >= 2 && s[len(s)-1] == '"' {
			return s[1 : len(s)-1], true
		}

		return "", false
	}

	return s, true
}

func isAlpha(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

func isNumber(r rune) bool {
	return r >= '0' && r <= '9'
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t'
}

func isNewline(r rune) bool {
	return r == '\r' || r == '\n'
}

func isValidMessageRole(role string) bool {
	return role == "system" || role == "user" || role == "assistant"
}

func isValidCommand(cmd string) bool {
	switch strings.ToLower(cmd) {
	case "from", "license", "template", "system", "adapter", "parameter", "message":
		return true
	default:
		return false
	}
}

// 处理路径中的 ~ 符号（如 "~/models" → "/home/user/models"）
func expandPathImpl(path, relativeDir string, currentUserFunc func() (*user.User, error), lookupUserFunc func(string) (*user.User, error)) (string, error) {
	if filepath.IsAbs(path) || strings.HasPrefix(path, "\\") || strings.HasPrefix(path, "/") {
		return filepath.Abs(path)
	} else if strings.HasPrefix(path, "~") {
		var homeDir string

		if path == "~" || strings.HasPrefix(path, "~/") {
			// Current user's home directory
			currentUser, err := currentUserFunc()
			if err != nil {
				return "", fmt.Errorf("failed to get current user: %w", err)
			}
			homeDir = currentUser.HomeDir
			path = strings.TrimPrefix(path, "~")
		} else {
			// Specific user's home directory
			parts := strings.SplitN(path[1:], "/", 2)
			userInfo, err := lookupUserFunc(parts[0])
			if err != nil {
				return "", fmt.Errorf("failed to find user '%s': %w", parts[0], err)
			}
			homeDir = userInfo.HomeDir
			if len(parts) > 1 {
				path = "/" + parts[1]
			} else {
				path = ""
			}
		}

		path = filepath.Join(homeDir, path)
	} else {
		path = filepath.Join(relativeDir, path)
	}

	return filepath.Abs(path)
}

func expandPath(path, relativeDir string) (string, error) {
	return expandPathImpl(path, relativeDir, user.Current, user.Lookup)
}
