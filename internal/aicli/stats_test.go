package aicli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUsage 验证 GET /v1/stats/usage 的请求与响应解析。
func TestUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/stats/usage" {
			t.Errorf("请求路径不匹配: got=%s want=/v1/stats/usage", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("请求方法不匹配: got=%s want=GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization 头不匹配: got=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{
					"model":             "claude-x",
					"prompt_tokens":     100,
					"completion_tokens": 200,
					"total_tokens":      300,
					"requests":          5,
					"usd":               0.12,
				},
			},
			"total": map[string]any{
				"prompt_tokens":     100,
				"completion_tokens": 200,
				"total_tokens":      300,
				"requests":          5,
				"usd":               0.12,
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "k")
	resp, err := c.Usage(context.Background())
	if err != nil {
		t.Fatalf("Usage 失败: %v", err)
	}
	if len(resp.Models) != 1 {
		t.Fatalf("models 长度期望 1, got=%d", len(resp.Models))
	}
	m := resp.Models[0]
	if m.Model != "claude-x" || m.PromptToks != 100 || m.CompToks != 200 || m.TotalToks != 300 || m.Requests != 5 || m.USD != 0.12 {
		t.Errorf("model 字段不匹配: %+v", m)
	}
	if resp.Total.TotalToks != 300 || resp.Total.Requests != 5 || resp.Total.USD != 0.12 {
		t.Errorf("total 字段不匹配: %+v", resp.Total)
	}
}

// TestPrices 验证 GET /v1/stats/prices 的响应解析。
func TestPrices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/stats/prices" {
			t.Errorf("请求路径不匹配: got=%s want=/v1/stats/prices", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("请求方法不匹配: got=%s want=GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{
					"model":             "claude-x",
					"prompt_per_1m":     3.0,
					"completion_per_1m": 15.0,
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "k")
	resp, err := c.Prices(context.Background())
	if err != nil {
		t.Fatalf("Prices 失败: %v", err)
	}
	if len(resp.Models) != 1 {
		t.Fatalf("models 长度期望 1, got=%d", len(resp.Models))
	}
	p := resp.Models[0]
	if p.Model != "claude-x" || p.PromptPer1M != 3.0 || p.CompPer1M != 15.0 {
		t.Errorf("price 字段不匹配: %+v", p)
	}
}

// TestSummary 验证 GET /v1/stats/summary 的响应解析。
func TestSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/stats/summary" {
			t.Errorf("请求路径不匹配: got=%s want=/v1/stats/summary", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("请求方法不匹配: got=%s want=GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_requests": 42,
			"total_tokens":   1024,
			"total_usd":      1.5,
			"uptime":         "3h",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "k")
	resp, err := c.Summary(context.Background())
	if err != nil {
		t.Fatalf("Summary 失败: %v", err)
	}
	if resp.TotalRequests != 42 || resp.TotalTokens != 1024 || resp.TotalUSD != 1.5 || resp.Uptime != "3h" {
		t.Errorf("summary 字段不匹配: %+v", resp)
	}
}

// TestConcurrency 验证 GET /v1/stats/concurrency 的响应解析。
func TestConcurrency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/stats/concurrency" {
			t.Errorf("请求路径不匹配: got=%s want=/v1/stats/concurrency", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("请求方法不匹配: got=%s want=GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": 3,
			"queued": 1,
			"max":    10,
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "k")
	resp, err := c.Concurrency(context.Background())
	if err != nil {
		t.Fatalf("Concurrency 失败: %v", err)
	}
	if resp.Active != 3 || resp.Queued != 1 || resp.Max != 10 {
		t.Errorf("concurrency 字段不匹配: %+v", resp)
	}
}
