package aicli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestModels 验证 GET /v1/models 的请求与响应解析。
func TestModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("请求路径不匹配: got=%s want=/v1/models", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("请求方法不匹配: got=%s want=GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization 头不匹配: got=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":       "claude/anthropic/claude-sonnet-4.5",
					"object":   "model",
					"owned_by": "anthropic",
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "k")
	resp, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models 失败: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("data 长度期望 1, got=%d", len(resp.Data))
	}
	m := resp.Data[0]
	if m.ID != "claude/anthropic/claude-sonnet-4.5" {
		t.Errorf("id 不匹配: got=%q", m.ID)
	}
	if m.Object != "model" {
		t.Errorf("object 不匹配: got=%q", m.Object)
	}
	if m.OwnedBy != "anthropic" {
		t.Errorf("owned_by 不匹配: got=%q", m.OwnedBy)
	}
}
