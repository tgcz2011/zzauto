package aicli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRunStream_OK 验证 POST /v1/runs 的 SSE 流解析、事件回调与 runID 提取。
func TestRunStream_OK(t *testing.T) {
	events := []string{
		`data: {"type":"system","run_id":"run-123"}`,
		`data: {"type":"text","content":"hello"}`,
		`data: {"type":"result","run_id":"run-123"}`,
		`data: [DONE]`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs" {
			t.Errorf("请求路径不匹配: got=%s want=/v1/runs", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("请求方法不匹配: got=%s want=POST", r.Method)
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept 头不匹配: got=%q want=text/event-stream", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization 头不匹配: got=%q", got)
		}
		// 校验请求体
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("解析请求体失败: %v", err)
		}
		if req["model"] != DefaultModel {
			t.Errorf("model 不匹配: got=%v want=%q", req["model"], DefaultModel)
		}
		if req["stream"] != true {
			t.Errorf("stream 应为 true, got=%v", req["stream"])
		}
		if req["system"] != "sys" {
			t.Errorf("system 不匹配: got=%v", req["system"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter 不支持 Flush")
		}
		for _, e := range events {
			fmt.Fprintln(w, e)
			fmt.Fprintln(w)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "k")
	var got []RunEvent
	runID, err := c.RunStream(context.Background(), "", "sys", "hi", func(evt RunEvent) error {
		got = append(got, evt)
		return nil
	})
	if err != nil {
		t.Fatalf("RunStream 失败: %v", err)
	}
	if runID != "run-123" {
		t.Errorf("runID 不匹配: got=%q want=%q", runID, "run-123")
	}
	if len(got) != 3 {
		t.Fatalf("事件数期望 3, got=%d", len(got))
	}
	if got[0].Type != "system" || got[0].RunID != "run-123" {
		t.Errorf("事件 0 不匹配: %+v", got[0])
	}
	if got[1].Type != "text" || got[1].Content != "hello" {
		t.Errorf("事件 1 不匹配: %+v", got[1])
	}
	if got[2].Type != "result" || got[2].RunID != "run-123" {
		t.Errorf("事件 2 不匹配: %+v", got[2])
	}
}

// TestRunStream_ExplicitModel 验证传入 model 时不使用 c.model。
func TestRunStream_ExplicitModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if req["model"] != "custom-model" {
			t.Errorf("model 不匹配: got=%v want=custom-model", req["model"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"type":"system","run_id":"r1"}`)
		fmt.Fprintln(w)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "k")
	runID, err := c.RunStream(context.Background(), "custom-model", "s", "u", nil)
	if err != nil {
		t.Fatalf("RunStream 失败: %v", err)
	}
	if runID != "r1" {
		t.Errorf("runID 不匹配: got=%q want=r1", runID)
	}
}

// TestRunStream_CallbackError 验证 onEvent 返回 error 时立即终止流。
func TestRunStream_CallbackError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"type":"system","run_id":"r9"}`)
		fmt.Fprintln(w)
		flusher.Flush()
		fmt.Fprintln(w, `data: {"type":"text","content":"more"}`)
		fmt.Fprintln(w)
		flusher.Flush()
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "k")
	stopErr := fmt.Errorf("stop")
	var count int
	runID, err := c.RunStream(context.Background(), "", "s", "u", func(evt RunEvent) error {
		count++
		return stopErr
	})
	if !errorIs(err, stopErr) {
		t.Errorf("期望回调错误向上传递, got=%v", err)
	}
	if count != 1 {
		t.Errorf("期望回调只被调用 1 次, got=%d", count)
	}
	if runID != "r9" {
		t.Errorf("runID 不匹配: got=%q want=r9", runID)
	}
}

// errorIs 简单比较错误（避免引入 errors.Is 增加导入）。
func errorIs(err, target error) bool {
	return err != nil && err.Error() == target.Error()
}

// TestGetRun 验证 GET /v1/runs/{id} 的请求与响应解析。
func TestGetRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/v1/runs/run-abc"
		if r.URL.Path != wantPath {
			t.Errorf("请求路径不匹配: got=%s want=%s", r.URL.Path, wantPath)
		}
		if r.Method != http.MethodGet {
			t.Errorf("请求方法不匹配: got=%s want=GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization 头不匹配: got=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "run-abc",
			"model":      "claude-x",
			"status":     "completed",
			"created_at": "2025-01-01T00:00:00Z",
			"events": []map[string]any{
				{"type": "text", "content": "hi"},
				{"type": "result", "run_id": "run-abc"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "k")
	detail, err := c.GetRun(context.Background(), "run-abc")
	if err != nil {
		t.Fatalf("GetRun 失败: %v", err)
	}
	if detail.ID != "run-abc" || detail.Model != "claude-x" || detail.Status != "completed" {
		t.Errorf("detail 基本字段不匹配: %+v", detail)
	}
	if detail.CreatedAt != "2025-01-01T00:00:00Z" {
		t.Errorf("created_at 不匹配: %q", detail.CreatedAt)
	}
	if len(detail.Events) != 2 {
		t.Fatalf("events 长度期望 2, got=%d", len(detail.Events))
	}
	if detail.Events[0].Type != "text" || detail.Events[0].Content != "hi" {
		t.Errorf("event 0 不匹配: %+v", detail.Events[0])
	}
	if detail.Events[1].Type != "result" || detail.Events[1].RunID != "run-abc" {
		t.Errorf("event 1 不匹配: %+v", detail.Events[1])
	}
}
