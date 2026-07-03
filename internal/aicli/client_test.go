package aicli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient 基于 httptest server 地址创建客户端（与生产 New 同路径）。
func newTestClient(t *testing.T, addr, apiKey string) *Client {
	t.Helper()
	return New(addr, apiKey)
}

// TestChat 验证 Chat 构造的请求（路径/方法/鉴权头/请求体）与响应解析均正确。
func TestChat(t *testing.T) {
	want := "你好，我是助手"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("请求路径不匹配: got=%s want=/v1/chat/completions", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("请求方法不匹配: got=%s want=POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization 头不匹配: got=%q want=%q", got, "Bearer test-key")
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type 不匹配: got=%q", ct)
		}

		// 校验请求体结构与字段
		body, _ := io.ReadAll(r.Body)
		var req openaiRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("解析请求体失败: %v", err)
		}
		if req.Model != DefaultModel {
			t.Errorf("model 不匹配: got=%q want=%q", req.Model, DefaultModel)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("messages 长度期望 2, got=%d", len(req.Messages))
		}
		if req.Messages[0].Role != "system" || req.Messages[0].Content != "你是助手" {
			t.Errorf("system 消息不匹配: %+v", req.Messages[0])
		}
		if req.Messages[1].Role != "user" || req.Messages[1].Content != "你好" {
			t.Errorf("user 消息不匹配: %+v", req.Messages[1])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": want}},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "test-key")
	got, err := c.Chat(context.Background(), "你是助手", "你好")
	if err != nil {
		t.Fatalf("Chat 失败: %v", err)
	}
	if got != want {
		t.Errorf("Chat 返回不匹配: got=%q want=%q", got, want)
	}
}

// TestChatEmptyChoices 验证空 choices 时返回明确错误。
func TestChatEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "test-key")
	_, err := c.Chat(context.Background(), "s", "u")
	if err == nil {
		t.Fatal("期望空 choices 错误，但返回 nil")
	}
	if !strings.Contains(err.Error(), "空 choices") {
		t.Errorf("错误应提示空 choices: %v", err)
	}
}

// TestChatError 验证非 2xx 响应返回包含状态码与响应体的错误。
func TestChatError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "test-key")
	_, err := c.Chat(context.Background(), "sys", "u")
	if err == nil {
		t.Fatal("期望错误，但返回 nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("错误应包含状态码 500: %v", err)
	}
	if !strings.Contains(err.Error(), "internal error") {
		t.Errorf("错误应包含响应体: %v", err)
	}
}

// TestMessages 验证 Anthropic 兼容接口的请求体与响应解析。
func TestMessages(t *testing.T) {
	want := "anthropic reply"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("请求路径不匹配: got=%s want=/v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization 头不匹配: got=%q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req anthropicRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("解析请求体失败: %v", err)
		}
		if req.Model != DefaultModel {
			t.Errorf("model 不匹配: got=%q want=%q", req.Model, DefaultModel)
		}
		if req.MaxTokens != DefaultMaxTokens {
			t.Errorf("max_tokens 不匹配: got=%d want=%d", req.MaxTokens, DefaultMaxTokens)
		}
		if req.System != "SYS" {
			t.Errorf("system 不匹配: got=%q want=%q", req.System, "SYS")
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "hi" {
			t.Errorf("messages 不匹配: %+v", req.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": want},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "k")
	got, err := c.Messages(context.Background(), "SYS", "hi")
	if err != nil {
		t.Fatalf("Messages 失败: %v", err)
	}
	if got != want {
		t.Errorf("Messages 返回不匹配: got=%q want=%q", got, want)
	}
}

// TestHealthOK 验证健康检查成功路径。
func TestHealthOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("请求路径不匹配: got=%s want=/healthz", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("请求方法不匹配: got=%s want=GET", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "test-key")
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health 期望成功: %v", err)
	}
}

// TestHealthFail 验证非 2xx 时健康检查返回错误且含状态码。
func TestHealthFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("service down"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "test-key")
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("Health 期望失败但返回 nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("错误应包含状态码 503: %v", err)
	}
	if !strings.Contains(err.Error(), "service down") {
		t.Errorf("错误应包含响应体: %v", err)
	}
}

// TestHealthUnreachable 验证服务不可达时返回包装后的网络错误。
func TestHealthUnreachable(t *testing.T) {
	// 启动后立即关闭，使该地址处于不可达状态。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()

	c := New(addr, "test-key")
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("期望不可达错误，但返回 nil")
	}
	if !strings.Contains(err.Error(), "不可达") {
		t.Errorf("错误应提示不可达: %v", err)
	}
}

// TestAskDelegatesToChat 验证 Ask 默认委托给 Chat。
func TestAskDelegatesToChat(t *testing.T) {
	want := "ask reply"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("Ask 应走 /v1/chat/completions, got=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": want}},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "test-key")
	got, err := c.Ask(context.Background(), "s", "u")
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if got != want {
		t.Errorf("Ask 返回不匹配: got=%q want=%q", got, want)
	}
}

// TestNewNormalizesAddr 验证 New 能容忍带协议前缀或尾部斜杠的地址。
func TestNewNormalizesAddr(t *testing.T) {
	c := New("http://127.0.0.1:8787/", "k")
	if c.addr != "127.0.0.1:8787" {
		t.Errorf("addr 规范化失败: got=%q want=%q", c.addr, "127.0.0.1:8787")
	}
	if got := c.baseURL(); got != "http://127.0.0.1:8787" {
		t.Errorf("baseURL 不匹配: got=%q", got)
	}
}
