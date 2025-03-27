package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/bnulwh/ollama/envconfig"
)

// StatusError 封装 HTTP 状态码和错误消息
// StatusError is an error with an HTTP status code and message.
type StatusError struct {
	StatusCode   int    // HTTP 状态码（如 404）
	Status       string // HTTP 状态文本（如 "Not Found"）
	ErrorMessage string `json:"error"` // 服务端返回的错误消息
}

// 错误信息格式化
func (e StatusError) Error() string {
	switch {
	case e.Status != "" && e.ErrorMessage != "":
		return fmt.Sprintf("%s: %s", e.Status, e.ErrorMessage)
	case e.Status != "":
		return e.Status
	case e.ErrorMessage != "": // 优先返回服务端错误消息
		return e.ErrorMessage
	default:
		// this should not happen
		return "something went wrong, please see the ollama server logs for details"
	}
}

// ImageData represents the raw binary data of an image file.
type ImageData []byte

// 用于向模型发送生成请求
// GenerateRequest describes a request sent by [Client.Generate]. While you
// have to specify the Model and Prompt fields, all the other fields have
// reasonable defaults for basic uses.
type GenerateRequest struct {
	// Model is the model name; it should be a name familiar to Ollama from
	// the library at https://ollama.com/library
	Model string `json:"model"` // 模型名称（如 "llama3"）

	// Prompt is the textual prompt to send to the model.
	Prompt string `json:"prompt"` // 用户输入的提示文本

	// Suffix is the text that comes after the inserted text.
	Suffix string `json:"suffix"`

	// System overrides the model's default system message/prompt.
	System string `json:"system"` // 覆盖模型的默认系统消息

	// Template overrides the model's default prompt template.
	Template string `json:"template"`

	// Context is the context parameter returned from a previous call to
	// [Client.Generate]. It can be used to keep a short conversational memory.
	Context []int `json:"context,omitempty"` // 上下文 token（用于多轮对话）

	// Stream specifies whether the response is streaming; it is true by default.
	Stream *bool `json:"stream,omitempty"` // 是否启用流式响应（默认 true）

	// Raw set to true means that no formatting will be applied to the prompt.
	Raw bool `json:"raw,omitempty"`

	// Format specifies the format to return a response in.
	Format json.RawMessage `json:"format,omitempty"`

	// KeepAlive controls how long the model will stay loaded in memory following
	// this request.
	KeepAlive *Duration `json:"keep_alive,omitempty"`

	// Images is an optional list of base64-encoded images accompanying this
	// request, for multimodal models.
	Images []ImageData `json:"images,omitempty"` // 多模态模型的图像数据（Base64）

	// Options lists model-specific options. For example, temperature can be
	// set through this field, if the model supports it.
	Options map[string]interface{} `json:"options"` // 模型参数（如温度、top-p）
}

// 用于多轮对话请求。
// ChatRequest describes a request sent by [Client.Chat].
type ChatRequest struct {
	// Model is the model name, as in [GenerateRequest].
	Model string `json:"model"` // 模型名称

	// Messages is the messages of the chat - can be used to keep a chat memory.
	Messages []Message `json:"messages"` // 消息历史（角色、内容、图像）

	// Stream enables streaming of returned responses; true by default.
	Stream *bool `json:"stream,omitempty"`

	// Format is the format to return the response in (e.g. "json").
	Format json.RawMessage `json:"format,omitempty"`

	// KeepAlive controls how long the model will stay loaded into memory
	// following the request.
	KeepAlive *Duration `json:"keep_alive,omitempty"`

	// Tools is an optional list of tools the model has access to.
	Tools `json:"tools,omitempty"` // 模型可调用的工具列表

	// Options lists model-specific options.
	Options map[string]interface{} `json:"options"`
}

type Tools []Tool

func (t Tools) String() string {
	bts, _ := json.Marshal(t)
	return string(bts)
}

func (t Tool) String() string {
	bts, _ := json.Marshal(t)
	return string(bts)
}

// 表示单条聊天消息
// Message is a single message in a chat sequence. The message contains the
// role ("system", "user", or "assistant"), the content and an optional list
// of images.
type Message struct {
	Role      string      `json:"role"`                 // 角色（"system", "user", "assistant"）
	Content   string      `json:"content"`              // 消息内容
	Images    []ImageData `json:"images,omitempty"`     // 附加图像
	ToolCalls []ToolCall  `json:"tool_calls,omitempty"` // 模型调用的工具列表
}

// 反序列化时统一角色为小写
func (m *Message) UnmarshalJSON(b []byte) error {
	type Alias Message
	var a Alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}

	*m = Message(a)
	m.Role = strings.ToLower(m.Role)
	return nil
}

type ToolCall struct {
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Index     int                       `json:"index,omitempty"`
	Name      string                    `json:"name"`
	Arguments ToolCallFunctionArguments `json:"arguments"`
}

type ToolCallFunctionArguments map[string]any

func (t *ToolCallFunctionArguments) String() string {
	bts, _ := json.Marshal(t)
	return string(bts)
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  struct {
		Type       string   `json:"type"`
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type        string   `json:"type"`
			Description string   `json:"description"`
			Enum        []string `json:"enum,omitempty"`
		} `json:"properties"`
	} `json:"parameters"`
}

func (t *ToolFunction) String() string {
	bts, _ := json.Marshal(t)
	return string(bts)
}

// ChatResponse is the response returned by [Client.Chat]. Its fields are
// similar to [GenerateResponse].
type ChatResponse struct {
	Model      string    `json:"model"`
	CreatedAt  time.Time `json:"created_at"`
	Message    Message   `json:"message"`
	DoneReason string    `json:"done_reason,omitempty"`

	Done bool `json:"done"`

	Metrics
}

// 性能指标
type Metrics struct {
	TotalDuration      time.Duration `json:"total_duration,omitempty"` // 总耗时
	LoadDuration       time.Duration `json:"load_duration,omitempty"`
	PromptEvalCount    int           `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration time.Duration `json:"prompt_eval_duration,omitempty"`
	EvalCount          int           `json:"eval_count,omitempty"` // 生成的 token 数
	EvalDuration       time.Duration `json:"eval_duration,omitempty"`
}

// 控制模型生成行为的参数
// Options specified in [GenerateRequest].  If you add a new option here, also
// add it to the API docs.
type Options struct {
	Runner

	// Predict options used at runtime
	NumKeep          int      `json:"num_keep,omitempty"`
	Seed             int      `json:"seed,omitempty"` // 随机种子（-1 表示随机）
	NumPredict       int      `json:"num_predict,omitempty"`
	TopK             int      `json:"top_k,omitempty"` // 采样时保留的最高概率 token 数
	TopP             float32  `json:"top_p,omitempty"` // 累积概率阈值（nucleus sampling）
	MinP             float32  `json:"min_p,omitempty"`
	TypicalP         float32  `json:"typical_p,omitempty"`
	RepeatLastN      int      `json:"repeat_last_n,omitempty"`
	Temperature      float32  `json:"temperature,omitempty"` // 温度（控制随机性）
	RepeatPenalty    float32  `json:"repeat_penalty,omitempty"`
	PresencePenalty  float32  `json:"presence_penalty,omitempty"`
	FrequencyPenalty float32  `json:"frequency_penalty,omitempty"`
	Mirostat         int      `json:"mirostat,omitempty"`
	MirostatTau      float32  `json:"mirostat_tau,omitempty"`
	MirostatEta      float32  `json:"mirostat_eta,omitempty"`
	Stop             []string `json:"stop,omitempty"` // 停止生成的 token 列表
}

// 模型加载时的硬件和内存配置
// Runner options which must be set when the model is loaded into memory
type Runner struct {
	NumCtx    int   `json:"num_ctx,omitempty"` // 上下文长度（token 数）
	NumBatch  int   `json:"num_batch,omitempty"`
	NumGPU    int   `json:"num_gpu,omitempty"` // 使用的 GPU 数量
	MainGPU   int   `json:"main_gpu,omitempty"`
	LowVRAM   bool  `json:"low_vram,omitempty"`
	F16KV     bool  `json:"f16_kv,omitempty"` // Deprecated: This option is ignored
	LogitsAll bool  `json:"logits_all,omitempty"`
	VocabOnly bool  `json:"vocab_only,omitempty"`
	UseMMap   *bool `json:"use_mmap,omitempty"`
	UseMLock  bool  `json:"use_mlock,omitempty"` // 是否锁定内存（避免交换）
	NumThread int   `json:"num_thread,omitempty"`
}

// EmbedRequest is the request passed to [Client.Embed].
type EmbedRequest struct {
	// Model is the model name.
	Model string `json:"model"`

	// Input is the input to embed.
	Input any `json:"input"`

	// KeepAlive controls how long the model will stay loaded in memory following
	// this request.
	KeepAlive *Duration `json:"keep_alive,omitempty"`

	Truncate *bool `json:"truncate,omitempty"`

	// Options lists model-specific options.
	Options map[string]interface{} `json:"options"`
}

// EmbedResponse is the response from [Client.Embed].
type EmbedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`

	TotalDuration   time.Duration `json:"total_duration,omitempty"`
	LoadDuration    time.Duration `json:"load_duration,omitempty"`
	PromptEvalCount int           `json:"prompt_eval_count,omitempty"`
}

// EmbeddingRequest is the request passed to [Client.Embeddings].
type EmbeddingRequest struct {
	// Model is the model name.
	Model string `json:"model"`

	// Prompt is the textual prompt to embed.
	Prompt string `json:"prompt"`

	// KeepAlive controls how long the model will stay loaded in memory following
	// this request.
	KeepAlive *Duration `json:"keep_alive,omitempty"`

	// Options lists model-specific options.
	Options map[string]interface{} `json:"options"`
}

// EmbeddingResponse is the response from [Client.Embeddings].
type EmbeddingResponse struct {
	Embedding []float64 `json:"embedding"`
}

// CreateRequest is the request passed to [Client.Create].
type CreateRequest struct {
	Model    string `json:"model"`
	Stream   *bool  `json:"stream,omitempty"`
	Quantize string `json:"quantize,omitempty"`

	From       string            `json:"from,omitempty"`
	Files      map[string]string `json:"files,omitempty"`
	Adapters   map[string]string `json:"adapters,omitempty"`
	Template   string            `json:"template,omitempty"`
	License    any               `json:"license,omitempty"`
	System     string            `json:"system,omitempty"`
	Parameters map[string]any    `json:"parameters,omitempty"`
	Messages   []Message         `json:"messages,omitempty"`

	// Deprecated: set the model name with Model instead
	Name string `json:"name"`
	// Deprecated: use Quantize instead
	Quantization string `json:"quantization,omitempty"`
}

// DeleteRequest is the request passed to [Client.Delete].
type DeleteRequest struct {
	Model string `json:"model"`

	// Deprecated: set the model name with Model instead
	Name string `json:"name"`
}

// ShowRequest is the request passed to [Client.Show].
type ShowRequest struct {
	Model  string `json:"model"`
	System string `json:"system"`

	// Template is deprecated
	Template string `json:"template"`
	Verbose  bool   `json:"verbose"`

	Options map[string]interface{} `json:"options"`

	// Deprecated: set the model name with Model instead
	Name string `json:"name"`
}

// ShowResponse is the response returned from [Client.Show].
type ShowResponse struct {
	License       string         `json:"license,omitempty"`
	Modelfile     string         `json:"modelfile,omitempty"`
	Parameters    string         `json:"parameters,omitempty"`
	Template      string         `json:"template,omitempty"`
	System        string         `json:"system,omitempty"`
	Details       ModelDetails   `json:"details,omitempty"`
	Messages      []Message      `json:"messages,omitempty"`
	ModelInfo     map[string]any `json:"model_info,omitempty"`
	ProjectorInfo map[string]any `json:"projector_info,omitempty"`
	Tensors       []Tensor       `json:"tensors,omitempty"`
	ModifiedAt    time.Time      `json:"modified_at,omitempty"`
}

// CopyRequest is the request passed to [Client.Copy].
type CopyRequest struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// PullRequest is the request passed to [Client.Pull].
type PullRequest struct {
	Model    string `json:"model"`
	Insecure bool   `json:"insecure,omitempty"` // Deprecated: ignored
	Username string `json:"username"`           // Deprecated: ignored
	Password string `json:"password"`           // Deprecated: ignored
	Stream   *bool  `json:"stream,omitempty"`

	// Deprecated: set the model name with Model instead
	Name string `json:"name"`
}

// ProgressResponse is the response passed to progress functions like
// [PullProgressFunc] and [PushProgressFunc].
type ProgressResponse struct {
	Status    string `json:"status"`
	Digest    string `json:"digest,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Completed int64  `json:"completed,omitempty"`
}

// PushRequest is the request passed to [Client.Push].
type PushRequest struct {
	Model    string `json:"model"`
	Insecure bool   `json:"insecure,omitempty"`
	Username string `json:"username"`
	Password string `json:"password"`
	Stream   *bool  `json:"stream,omitempty"`

	// Deprecated: set the model name with Model instead
	Name string `json:"name"`
}

// 列出本地所有模型的信息
// ListResponse is the response from [Client.List].
type ListResponse struct {
	Models []ListModelResponse `json:"models"` // 模型列表
}

// ProcessResponse is the response from [Client.Process].
type ProcessResponse struct {
	Models []ProcessModelResponse `json:"models"`
}

// 模型的信息
// ListModelResponse is a single model description in [ListResponse].
type ListModelResponse struct {
	Name       string       `json:"name"`  // 模型名称
	Model      string       `json:"model"` // 模型名称
	ModifiedAt time.Time    `json:"modified_at"`
	Size       int64        `json:"size"`   // 模型文件大小
	Digest     string       `json:"digest"` // 模型哈希（SHA256）
	Details    ModelDetails `json:"details,omitempty"`
}

// ProcessModelResponse is a single model description in [ProcessResponse].
type ProcessModelResponse struct {
	Name      string       `json:"name"`
	Model     string       `json:"model"`
	Size      int64        `json:"size"`
	Digest    string       `json:"digest"`
	Details   ModelDetails `json:"details,omitempty"`
	ExpiresAt time.Time    `json:"expires_at"`
	SizeVRAM  int64        `json:"size_vram"`
}

type RetrieveModelResponse struct {
	Id      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type TokenResponse struct {
	Token string `json:"token"`
}

// 包含模型的生成结果
// GenerateResponse is the response passed into [GenerateResponseFunc].
type GenerateResponse struct {
	// Model is the model name that generated the response.
	Model string `json:"model"`

	// CreatedAt is the timestamp of the response.
	CreatedAt time.Time `json:"created_at"`

	// Response is the textual response itself.
	Response string `json:"response"` // 生成的文本

	// Done specifies if the response is complete.
	Done bool `json:"done"` // 是否生成完成

	// DoneReason is the reason the model stopped generating text.
	DoneReason string `json:"done_reason,omitempty"`

	// Context is an encoding of the conversation used in this response; this
	// can be sent in the next request to keep a conversational memory.
	Context []int `json:"context,omitempty"` // 新的上下文 token（用于后续请求）

	Metrics // 耗时和 token 统计
}

// ModelDetails provides details about a model.
type ModelDetails struct {
	ParentModel       string   `json:"parent_model"`
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// Tensor describes the metadata for a given tensor.
type Tensor struct {
	Name  string   `json:"name"`
	Type  string   `json:"type"`
	Shape []uint64 `json:"shape"`
}

func (m *Metrics) Summary() {
	if m.TotalDuration > 0 {
		fmt.Fprintf(os.Stderr, "total duration:       %v\n", m.TotalDuration)
	}

	if m.LoadDuration > 0 {
		fmt.Fprintf(os.Stderr, "load duration:        %v\n", m.LoadDuration)
	}

	if m.PromptEvalCount > 0 {
		fmt.Fprintf(os.Stderr, "prompt eval count:    %d token(s)\n", m.PromptEvalCount)
	}

	if m.PromptEvalDuration > 0 {
		fmt.Fprintf(os.Stderr, "prompt eval duration: %s\n", m.PromptEvalDuration)
		fmt.Fprintf(os.Stderr, "prompt eval rate:     %.2f tokens/s\n", float64(m.PromptEvalCount)/m.PromptEvalDuration.Seconds())
	}

	if m.EvalCount > 0 {
		fmt.Fprintf(os.Stderr, "eval count:           %d token(s)\n", m.EvalCount)
	}

	if m.EvalDuration > 0 {
		fmt.Fprintf(os.Stderr, "eval duration:        %s\n", m.EvalDuration)
		fmt.Fprintf(os.Stderr, "eval rate:            %.2f tokens/s\n", float64(m.EvalCount)/m.EvalDuration.Seconds())
	}
}

// 将 map 类型的参数转换为结构体字段
func (opts *Options) FromMap(m map[string]interface{}) error {
	valueOpts := reflect.ValueOf(opts).Elem() // names of the fields in the options struct
	typeOpts := reflect.TypeOf(opts).Elem()   // types of the fields in the options struct

	// build map of json struct tags to their types
	jsonOpts := make(map[string]reflect.StructField)
	for _, field := range reflect.VisibleFields(typeOpts) {
		jsonTag := strings.Split(field.Tag.Get("json"), ",")[0]
		if jsonTag != "" {
			jsonOpts[jsonTag] = field
		}
	}
	// 反射遍历字段，根据 JSON 标签匹配键值
	for key, val := range m {
		opt, ok := jsonOpts[key]
		if !ok {
			slog.Warn("invalid option provided", "option", key)
			continue
		}

		field := valueOpts.FieldByName(opt.Name)
		if field.IsValid() && field.CanSet() {
			if val == nil {
				continue
			}

			switch field.Kind() {
			case reflect.Int:
				switch t := val.(type) {
				case int64:
					field.SetInt(t)
				case float64:
					// when JSON unmarshals numbers, it uses float64, not int
					field.SetInt(int64(t))
				default:
					return fmt.Errorf("option %q must be of type integer", key)
				}
			case reflect.Bool:
				val, ok := val.(bool)
				if !ok {
					return fmt.Errorf("option %q must be of type boolean", key)
				}
				field.SetBool(val)
			case reflect.Float32:
				// JSON unmarshals to float64
				val, ok := val.(float64)
				if !ok {
					return fmt.Errorf("option %q must be of type float32", key)
				}
				field.SetFloat(val)
			case reflect.String:
				val, ok := val.(string)
				if !ok {
					return fmt.Errorf("option %q must be of type string", key)
				}
				field.SetString(val)
			case reflect.Slice:
				// JSON unmarshals to []interface{}, not []string
				val, ok := val.([]interface{})
				if !ok {
					return fmt.Errorf("option %q must be of type array", key)
				}
				// convert []interface{} to []string
				slice := make([]string, len(val))
				for i, item := range val {
					str, ok := item.(string)
					if !ok {
						return fmt.Errorf("option %q must be of an array of strings", key)
					}
					slice[i] = str
				}
				field.Set(reflect.ValueOf(slice))
			case reflect.Pointer:
				var b bool
				if field.Type() == reflect.TypeOf(&b) {
					val, ok := val.(bool)
					if !ok {
						return fmt.Errorf("option %q must be of type boolean", key)
					}
					field.Set(reflect.ValueOf(&val))
				} else {
					return fmt.Errorf("unknown type loading config params: %v %v", field.Kind(), field.Type())
				}
			default:
				return fmt.Errorf("unknown type loading config params: %v", field.Kind())
			}
		}
	}

	return nil
}

// 提供生成请求的默认参数
// DefaultOptions is the default set of options for [GenerateRequest]; these
// values are used unless the user specifies other values explicitly.
func DefaultOptions() Options {
	return Options{
		// options set on request to runner
		NumPredict: -1, // 无限生成，直到模型停止

		// set a minimal num_keep to avoid issues on context shifts
		NumKeep:          4,
		Temperature:      0.8,
		TopK:             40,
		TopP:             0.9,
		TypicalP:         1.0,
		RepeatLastN:      64,
		RepeatPenalty:    1.1,
		PresencePenalty:  0.0,
		FrequencyPenalty: 0.0,
		Mirostat:         0,
		MirostatTau:      5.0,
		MirostatEta:      0.1,
		Seed:             -1,

		Runner: Runner{
			// options set when the model is loaded
			NumCtx:    int(envconfig.ContextLength()), // 从环境变量获取默认上下文长度
			NumBatch:  512,
			NumGPU:    -1, // -1 here indicates that NumGPU should be set dynamically
			NumThread: 0,  // let the runtime decide
			LowVRAM:   false,
			UseMLock:  false,
			UseMMap:   nil,
		},
	}
}

// 自定义 JSON 序列化和反序列化逻辑
type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	if d.Duration < 0 {
		return []byte("-1"), nil
	}
	return []byte("\"" + d.Duration.String() + "\""), nil
}

func (d *Duration) UnmarshalJSON(b []byte) (err error) {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}

	d.Duration = 5 * time.Minute

	switch t := v.(type) {
	case float64:
		if t < 0 {
			d.Duration = time.Duration(math.MaxInt64)
		} else {
			d.Duration = time.Duration(int(t) * int(time.Second))
		}
	case string:
		// 支持字符串（如 "5m"）和数值（如 300 秒）
		d.Duration, err = time.ParseDuration(t)
		if err != nil {
			return err
		}
		if d.Duration < 0 {
			d.Duration = time.Duration(math.MaxInt64)
		}
	default:
		return fmt.Errorf("Unsupported type: '%s'", reflect.TypeOf(v))
	}

	return nil
}

// FormatParams converts specified parameter options to their correct types
func FormatParams(params map[string][]string) (map[string]interface{}, error) {
	opts := Options{}
	valueOpts := reflect.ValueOf(&opts).Elem() // names of the fields in the options struct
	typeOpts := reflect.TypeOf(opts)           // types of the fields in the options struct

	// build map of json struct tags to their types
	jsonOpts := make(map[string]reflect.StructField)
	for _, field := range reflect.VisibleFields(typeOpts) {
		jsonTag := strings.Split(field.Tag.Get("json"), ",")[0]
		if jsonTag != "" {
			jsonOpts[jsonTag] = field
		}
	}

	out := make(map[string]interface{})
	// iterate params and set values based on json struct tags
	for key, vals := range params {
		if opt, ok := jsonOpts[key]; !ok {
			return nil, fmt.Errorf("unknown parameter '%s'", key)
		} else {
			field := valueOpts.FieldByName(opt.Name)
			if field.IsValid() && field.CanSet() {
				switch field.Kind() {
				case reflect.Float32:
					floatVal, err := strconv.ParseFloat(vals[0], 32)
					if err != nil {
						return nil, fmt.Errorf("invalid float value %s", vals)
					}

					out[key] = float32(floatVal)
				case reflect.Int:
					intVal, err := strconv.ParseInt(vals[0], 10, 64)
					if err != nil {
						return nil, fmt.Errorf("invalid int value %s", vals)
					}

					out[key] = intVal
				case reflect.Bool:
					boolVal, err := strconv.ParseBool(vals[0])
					if err != nil {
						return nil, fmt.Errorf("invalid bool value %s", vals)
					}

					out[key] = boolVal
				case reflect.String:
					out[key] = vals[0]
				case reflect.Slice:
					// TODO: only string slices are supported right now
					out[key] = vals
				case reflect.Pointer:
					var b bool
					if field.Type() == reflect.TypeOf(&b) {
						boolVal, err := strconv.ParseBool(vals[0])
						if err != nil {
							return nil, fmt.Errorf("invalid bool value %s", vals)
						}
						out[key] = &boolVal
					} else {
						return nil, fmt.Errorf("unknown type %s for %s", field.Kind(), key)
					}
				default:
					return nil, fmt.Errorf("unknown type %s for %s", field.Kind(), key)
				}
			}
		}
	}

	return out, nil
}
