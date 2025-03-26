// Package api implements the client-side API for code wishing to interact
// with the ollama service. The methods of the [Client] type correspond to
// the ollama REST API as described in [the API documentation].
// The ollama command-line client itself uses this package to interact with
// the backend service.
//
// # Examples
//
// Several examples of using this package are available [in the GitHub
// repository].
//
// [the API documentation]: https://github.com/ollama/ollama/blob/main/docs/api.md
// [in the GitHub repository]: https://github.com/ollama/ollama/tree/main/api/examples
package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"

	"github.com/bnulwh/ollama/envconfig"
	"github.com/bnulwh/ollama/format"
	"github.com/bnulwh/ollama/version"
)

// Client 封装与 ollama 服务交互的客户端状态
// Client encapsulates client state for interacting with the ollama
// service. Use [ClientFromEnvironment] to create new Clients.
type Client struct {
	base *url.URL     // 服务基础 URL（如 http://localhost:11434）
	http *http.Client // HTTP客户端（默认使用 http.DefaultClient）
}

// 检查 HTTP 响应状态码并解析错误信息
func checkError(resp *http.Response, body []byte) error {
	if resp.StatusCode < http.StatusBadRequest {
		return nil
	}

	apiError := StatusError{StatusCode: resp.StatusCode}

	err := json.Unmarshal(body, &apiError) // 尝试解析错误详情
	if err != nil {
		// Use the full body as the message if we fail to decode a response.
		apiError.ErrorMessage = string(body) // 直接返回原始错误内容
	}

	return apiError
}

// 从环境变量 OLLAMA_HOST 创建客户端
// ClientFromEnvironment creates a new [Client] using configuration from the
// environment variable OLLAMA_HOST, which points to the network host and
// port on which the ollama service is listening. The format of this variable
// is:
//
//	<scheme>://<host>:<port>
//
// If the variable is not specified, a default ollama host and port will be
// used.
func ClientFromEnvironment() (*Client, error) {
	return &Client{
		base: envconfig.Host(), // 从环境变量解析服务地址
		http: http.DefaultClient,
	}, nil
}

// 手动创建客户端（直接指定 base 和 http）
func NewClient(base *url.URL, http *http.Client) *Client {
	return &Client{
		base: base,
		http: http,
	}
}

// 处理通用 HTTP 请求（同步）
func (c *Client) do(ctx context.Context, method, path string, reqData, respData any) error {
	var reqBody io.Reader
	var data []byte
	var err error

	switch reqData := reqData.(type) {
	case io.Reader: // 直接使用流式数据（如文件上传）
		// reqData is already an io.Reader
		reqBody = reqData
	case nil: // 无请求体（如 GET 请求）
		// noop
	default: // 结构体转换为 JSON
		data, err = json.Marshal(reqData)
		if err != nil {
			return err
		}

		reqBody = bytes.NewReader(data)
	}
	// 构造请求 URL 和请求对象
	requestURL := c.base.JoinPath(path)
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), reqBody)
	if err != nil {
		return err
	}
	// 设置请求头
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", fmt.Sprintf("ollama/%s (%s %s) Go/%s", version.Version, runtime.GOARCH, runtime.GOOS, runtime.Version()))
	// 发送请求并处理响应
	respObj, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer respObj.Body.Close()

	respBody, err := io.ReadAll(respObj.Body)
	if err != nil {
		return err
	}
	// 检查 HTTP 错误状态码
	if err := checkError(respObj, respBody); err != nil {
		return err
	}

	if len(respBody) > 0 && respData != nil {
		// 反序列化响应数据到指定结构体
		if err := json.Unmarshal(respBody, respData); err != nil {
			return err
		}
	}
	return nil
}

const maxBufferSize = 512 * format.KiloByte

// 处理流式 HTTP 请求（如生成、聊天等）
func (c *Client) stream(ctx context.Context, method, path string, data any, fn func([]byte) error) error {
	var buf io.Reader
	if data != nil {
		bts, err := json.Marshal(data)
		if err != nil {
			return err
		}
		// 将数据转为 JSON 流
		buf = bytes.NewBuffer(bts)
	}
	// 构造请求
	requestURL := c.base.JoinPath(path)
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), buf)
	if err != nil {
		return err
	}
	// 设置请求头（Accept 为流式格式）
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/x-ndjson")
	request.Header.Set("User-Agent", fmt.Sprintf("ollama/%s (%s %s) Go/%s", version.Version, runtime.GOARCH, runtime.GOOS, runtime.Version()))
	// 发送请求并逐行处理响应
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	scanner := bufio.NewScanner(response.Body)
	// increase the buffer size to avoid running out of space
	scanBuf := make([]byte, 0, maxBufferSize)
	scanner.Buffer(scanBuf, maxBufferSize) // 设置缓冲区大小（512KB）
	for scanner.Scan() {
		var errorResponse struct {
			Error string `json:"error,omitempty"`
		}

		bts := scanner.Bytes()
		if err := json.Unmarshal(bts, &errorResponse); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}

		if errorResponse.Error != "" {
			return errors.New(errorResponse.Error) // 处理服务器返回的错误
		}
		// 处理 HTTP 错误
		if response.StatusCode >= http.StatusBadRequest {
			return StatusError{
				StatusCode:   response.StatusCode,
				Status:       response.Status,
				ErrorMessage: errorResponse.Error,
			}
		}
		// 调用回调函数处理每行数据
		if err := fn(bts); err != nil {
			return err
		}
	}

	return nil
}

// GenerateResponseFunc is a function that [Client.Generate] invokes every time
// a response is received from the service. If this function returns an error,
// [Client.Generate] will stop generating and return this error.
type GenerateResponseFunc func(GenerateResponse) error

// 用于向模型发送提示并逐步接收生成结果。
// Generate generates a response for a given prompt. The req parameter should
// be populated with prompt details. fn is called for each response (there may
// be multiple responses, e.g. in case streaming is enabled).
func (c *Client) Generate(ctx context.Context, req *GenerateRequest, fn GenerateResponseFunc) error {
	return c.stream(ctx, http.MethodPost, "/api/generate", req, func(bts []byte) error {
		var resp GenerateResponse
		if err := json.Unmarshal(bts, &resp); err != nil {
			return err
		}

		return fn(resp) // 逐块回调生成的文本
	})
}

// ChatResponseFunc is a function that [Client.Chat] invokes every time
// a response is received from the service. If this function returns an error,
// [Client.Chat] will stop generating and return this error.
type ChatResponseFunc func(ChatResponse) error

// 维护聊天上下文并接收模型的回复。
// Chat generates the next message in a chat. [ChatRequest] may contain a
// sequence of messages which can be used to maintain chat history with a model.
// fn is called for each response (there may be multiple responses, e.g. if case
// streaming is enabled).
func (c *Client) Chat(ctx context.Context, req *ChatRequest, fn ChatResponseFunc) error {
	return c.stream(ctx, http.MethodPost, "/api/chat", req, func(bts []byte) error {
		var resp ChatResponse
		if err := json.Unmarshal(bts, &resp); err != nil {
			return err
		}

		return fn(resp) // 逐条回调聊天消息
	})
}

// PullProgressFunc is a function that [Client.Pull] invokes every time there
// is progress with a "pull" request sent to the service. If this function
// returns an error, [Client.Pull] will stop the process and return this error.
type PullProgressFunc func(ProgressResponse) error

// 从服务器下载模型并报告进度。
// Pull downloads a model from the ollama library. fn is called each time
// progress is made on the request and can be used to display a progress bar,
// etc.
func (c *Client) Pull(ctx context.Context, req *PullRequest, fn PullProgressFunc) error {
	return c.stream(ctx, http.MethodPost, "/api/pull", req, func(bts []byte) error {
		var resp ProgressResponse
		if err := json.Unmarshal(bts, &resp); err != nil {
			return err
		}

		return fn(resp) // 回调下载进度（如 50%）
	})
}

// PushProgressFunc is a function that [Client.Push] invokes when progress is
// made.
// It's similar to other progress function types like [PullProgressFunc].
type PushProgressFunc func(ProgressResponse) error

// Push uploads a model to the model library; requires registering for ollama.ai
// and adding a public key first. fn is called each time progress is made on
// the request and can be used to display a progress bar, etc.
func (c *Client) Push(ctx context.Context, req *PushRequest, fn PushProgressFunc) error {
	return c.stream(ctx, http.MethodPost, "/api/push", req, func(bts []byte) error {
		var resp ProgressResponse
		if err := json.Unmarshal(bts, &resp); err != nil {
			return err
		}

		return fn(resp)
	})
}

// CreateProgressFunc is a function that [Client.Create] invokes when progress
// is made.
// It's similar to other progress function types like [PullProgressFunc].
type CreateProgressFunc func(ProgressResponse) error

// Create creates a model from a [Modelfile]. fn is a progress function that
// behaves similarly to other methods (see [Client.Pull]).
//
// [Modelfile]: https://github.com/ollama/ollama/blob/main/docs/modelfile.md
func (c *Client) Create(ctx context.Context, req *CreateRequest, fn CreateProgressFunc) error {
	return c.stream(ctx, http.MethodPost, "/api/create", req, func(bts []byte) error {
		var resp ProgressResponse
		if err := json.Unmarshal(bts, &resp); err != nil {
			return err
		}

		return fn(resp)
	})
}

// 同步获取本地模型列表
// List lists models that are available locally.
func (c *Client) List(ctx context.Context) (*ListResponse, error) {
	var lr ListResponse
	if err := c.do(ctx, http.MethodGet, "/api/tags", nil, &lr); err != nil {
		return nil, err
	}
	return &lr, nil
}

// 同步获取本地运行模型列表
// ListRunning lists running models.
func (c *Client) ListRunning(ctx context.Context) (*ProcessResponse, error) {
	var lr ProcessResponse
	if err := c.do(ctx, http.MethodGet, "/api/ps", nil, &lr); err != nil {
		return nil, err
	}
	return &lr, nil
}

// Copy copies a model - creating a model with another name from an existing
// model.
func (c *Client) Copy(ctx context.Context, req *CopyRequest) error {
	if err := c.do(ctx, http.MethodPost, "/api/copy", req, nil); err != nil {
		return err
	}
	return nil
}

// 删除模型
// Delete deletes a model and its data.
func (c *Client) Delete(ctx context.Context, req *DeleteRequest) error {
	if err := c.do(ctx, http.MethodDelete, "/api/delete", req, nil); err != nil {
		return err
	}
	return nil
}

// Show obtains model information, including details, modelfile, license etc.
func (c *Client) Show(ctx context.Context, req *ShowRequest) (*ShowResponse, error) {
	var resp ShowResponse
	if err := c.do(ctx, http.MethodPost, "/api/show", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Heartbeat checks if the server has started and is responsive; if yes, it
// returns nil, otherwise an error.
func (c *Client) Heartbeat(ctx context.Context) error {
	if err := c.do(ctx, http.MethodHead, "/", nil, nil); err != nil {
		return err
	}
	return nil
}

// Embed generates embeddings from a model.
func (c *Client) Embed(ctx context.Context, req *EmbedRequest) (*EmbedResponse, error) {
	var resp EmbedResponse
	if err := c.do(ctx, http.MethodPost, "/api/embed", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Embeddings generates an embedding from a model.
func (c *Client) Embeddings(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	var resp EmbeddingResponse
	if err := c.do(ctx, http.MethodPost, "/api/embeddings", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateBlob creates a blob from a file on the server. digest is the
// expected SHA256 digest of the file, and r represents the file.
func (c *Client) CreateBlob(ctx context.Context, digest string, r io.Reader) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/blobs/%s", digest), r, nil)
}

// Version returns the Ollama server version as a string.
func (c *Client) Version(ctx context.Context) (string, error) {
	var version struct {
		Version string `json:"version"`
	}

	if err := c.do(ctx, http.MethodGet, "/api/version", nil, &version); err != nil {
		return "", err
	}

	return version.Version, nil
}
