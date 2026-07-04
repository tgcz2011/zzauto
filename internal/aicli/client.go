// Package aicli 封装 aiclibridge 的 HTTP 客户端。
//
// aiclibridge 是本地 HTTP 服务，暴露 OpenAI 兼容 /v1/chat/completions 与
// Anthropic 兼容 /v1/messages 接口。本包提供统一调用入口，供编排器与各 agent 使用。
package aicli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultModel 默认模型标识。
const DefaultModel = "claude/anthropic/claude-sonnet-4.5"

// DefaultMaxTokens Anthropic /v1/messages 接口默认最大输出 token 数。
const DefaultMaxTokens = 4096

// Client aiclibridge HTTP 客户端。
type Client struct {
	addr   string
	apiKey string
	http   *http.Client
	model  string
}

// New 创建 aiclibridge 客户端。addr 形如 "127.0.0.1:8787"。
func New(addr, apiKey string) *Client {
	return &Client{
		addr:   normalizeAddr(addr),
		apiKey: apiKey,
		http: &http.Client{
			Timeout: 5 * time.Minute, // AI 推理较慢，预留 5 分钟超时
		},
		model: DefaultModel,
	}
}

// normalizeAddr 去掉协议前缀与尾部斜杠，统一为 host:port 形式。
func normalizeAddr(addr string) string {
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	return strings.TrimRight(addr, "/")
}

// baseURL 返回 http://<addr> 前缀。
func (c *Client) baseURL() string {
	return "http://" + c.addr
}

// Health 探测 aiclibridge 健康状态，GET /healthz。返回 nil 表示可达。
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("构造健康检查请求失败: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("aiclibridge 不可达 (%s): %w", c.addr, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("健康检查失败: 状态码=%d 响应体=%s", resp.StatusCode, string(body))
	}
	return nil
}

// openaiMessage OpenAI 兼容接口的消息结构。
type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openaiRequest /v1/chat/completions 请求体。
type openaiRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
}

// openaiResponse /v1/chat/completions 响应体。
type openaiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Chat 调用 OpenAI 兼容接口 /v1/chat/completions。
// system 为系统提示，user 为用户输入，返回助手回复文本。
// 使用 c.model 作为请求模型。
func (c *Client) Chat(ctx context.Context, system, user string) (string, error) {
	return c.chatWithModel(ctx, c.model, system, user)
}

// chatWithModel 是 Chat 与 ChatWithModel 的共享实现，model 为本次请求使用的模型。
func (c *Client) chatWithModel(ctx context.Context, model, system, user string) (string, error) {
	reqBody := openaiRequest{
		Model: model,
		Messages: []openaiMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}
	data, err := c.doJSON(ctx, http.MethodPost, "/v1/chat/completions", reqBody)
	if err != nil {
		return "", err
	}
	var resp openaiResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("解析 /v1/chat/completions 响应失败: %w (body=%s)", err, string(data))
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return "", fmt.Errorf("aiclibridge 返回错误: %s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("aiclibridge 返回空 choices (body=%s)", string(data))
	}
	return resp.Choices[0].Message.Content, nil
}

// ChatWithModel 与 Chat 相同，但用传入 model 覆盖本次请求使用的模型（不修改 c.model）。
func (c *Client) ChatWithModel(ctx context.Context, model, system, user string) (string, error) {
	return c.chatWithModel(ctx, model, system, user)
}

// SetModel 设置客户端默认模型。
func (c *Client) SetModel(model string) {
	c.model = model
}

// Model 返回客户端当前默认模型。
func (c *Client) Model() string {
	return c.model
}

// anthropicMessage Anthropic 兼容接口的消息结构。
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicRequest /v1/messages 请求体。
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

// anthropicResponse /v1/messages 响应体。
type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Messages 调用 Anthropic 兼容接口 /v1/messages。
// system 为系统提示，user 为用户输入，返回 content[0].text。
func (c *Client) Messages(ctx context.Context, system, user string) (string, error) {
	reqBody := anthropicRequest{
		Model:     c.model,
		MaxTokens: DefaultMaxTokens,
		System:    system,
		Messages: []anthropicMessage{
			{Role: "user", Content: user},
		},
	}
	data, err := c.doJSON(ctx, http.MethodPost, "/v1/messages", reqBody)
	if err != nil {
		return "", err
	}
	var resp anthropicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("解析 /v1/messages 响应失败: %w (body=%s)", err, string(data))
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return "", fmt.Errorf("aiclibridge 返回错误: %s", resp.Error.Message)
	}
	if len(resp.Content) == 0 {
		return "", fmt.Errorf("aiclibridge 返回空 content (body=%s)", string(data))
	}
	return resp.Content[0].Text, nil
}

// Ask 高层便捷方法，默认走 OpenAI 兼容接口（即 Chat）。
// 该方法满足编排器 agents.AIClient 接口约定（鸭子类型，无需显式声明）。
func (c *Client) Ask(ctx context.Context, system, user string) (string, error) {
	return c.Chat(ctx, system, user)
}

// AskWithModel 与 Ask 相同，但用传入 model 覆盖本次请求使用的模型（委托 ChatWithModel）。
func (c *Client) AskWithModel(ctx context.Context, model, system, user string) (string, error) {
	return c.ChatWithModel(ctx, model, system, user)
}

// doJSON 发送 JSON 请求并返回响应体。
// 非 2xx 时返回包含状态码与响应体的错误；网络错误会被包装。
func (c *Client) doJSON(ctx context.Context, method, path string, body any) ([]byte, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL()+path, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 aiclibridge %s 失败: %w", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("aiclibridge %s 返回非 2xx: 状态码=%d 响应体=%s", path, resp.StatusCode, string(data))
	}
	return data, nil
}
