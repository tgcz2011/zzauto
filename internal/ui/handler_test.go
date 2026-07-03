package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tgcz2011/zzauto/internal/config"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// newTestHandler 构造一个用于测试的 Handler 及其依赖。
func newTestHandler(t *testing.T) (*Handler, *eventbus.Bus, *workspace.Workspace) {
	t.Helper()
	bus := eventbus.New()
	ws := workspace.New(t.TempDir(), "testproj")
	if err := ws.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	cfg := config.Default()
	return New(bus, ws, cfg), bus, ws
}

// TestInputWritesDoc 验证 POST /api/input 将需求写入 input.md 并附带 frontmatter。
func TestInputWritesDoc(t *testing.T) {
	h, _, ws := newTestHandler(t)
	body := strings.NewReader(`{"request":"做一个 todo 应用"}`)
	req := httptest.NewRequest("POST", "/api/input", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.handleInput(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	content, err := ws.ReadDoc("input.md")
	if err != nil {
		t.Fatalf("ReadDoc: %v", err)
	}
	if !strings.Contains(content, "做一个 todo 应用") {
		t.Fatalf("内容未包含需求: %s", content)
	}
	if !strings.Contains(content, "stage: listener") {
		t.Fatalf("缺少 frontmatter stage: %s", content)
	}
	if !strings.Contains(content, "status: pending") {
		t.Fatalf("缺少 frontmatter status: %s", content)
	}
}

// TestInputEmpty 验证空请求被拒绝。
func TestInputEmpty(t *testing.T) {
	h, _, _ := newTestHandler(t)
	req := httptest.NewRequest("POST", "/api/input", strings.NewReader(`{"request":"   "}`))
	rec := httptest.NewRecorder()
	h.handleInput(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// TestGetDoc 验证 GET /api/docs/{name} 返回文档内容。
func TestGetDoc(t *testing.T) {
	h, _, ws := newTestHandler(t)
	if err := ws.WriteDoc(workspace.DocDesire, "# 欲望\n我要一个 app"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/docs/desire", nil)
	req.SetPathValue("name", "desire")
	rec := httptest.NewRecorder()
	h.handleGetDoc(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["name"] != "desire" {
		t.Fatalf("name=%v", resp["name"])
	}
	raw, _ := resp["raw"].(string)
	if !strings.Contains(raw, "我要一个 app") {
		t.Fatalf("raw=%v", resp["raw"])
	}
}

// TestGetDocNotFound 验证不存在的文档返回 404。
func TestGetDocNotFound(t *testing.T) {
	h, _, _ := newTestHandler(t)
	req := httptest.NewRequest("GET", "/api/docs/need", nil)
	req.SetPathValue("name", "need")
	rec := httptest.NewRecorder()
	h.handleGetDoc(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// TestGetDocBadName 验证未知文档短名返回 400。
func TestGetDocBadName(t *testing.T) {
	h, _, _ := newTestHandler(t)
	req := httptest.NewRequest("GET", "/api/docs/unknown", nil)
	req.SetPathValue("name", "unknown")
	rec := httptest.NewRecorder()
	h.handleGetDoc(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// TestAskUser 验证 AskUser 发布问题后，POST /api/ask/{id} 能把答案送回。
func TestAskUser(t *testing.T) {
	h, bus, _ := newTestHandler(t)
	// 先订阅以捕获 ask_user 事件。
	sub := bus.Subscribe()

	type result struct {
		ans string
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		ans, err := h.AskUser("你的名字是？")
		resCh <- result{ans, err}
	}()

	var evt eventbus.Event
	select {
	case evt = <-sub:
	case <-time.After(time.Second):
		t.Fatal("未收到 ask_user 事件")
	}
	if evt.Type != eventbus.EventAskUser {
		t.Fatalf("事件类型=%s", evt.Type)
	}
	data, ok := evt.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data 类型错误: %T", evt.Data)
	}
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatal("id 为空")
	}
	if data["question"] != "你的名字是？" {
		t.Fatalf("question=%v", data["question"])
	}

	// 同时 /api/asks 应能列出该问题。
	listReq := httptest.NewRequest("GET", "/api/asks", nil)
	listRec := httptest.NewRecorder()
	h.handleListAsks(listRec, listReq)
	var listResp struct {
		Asks []askView `json:"asks"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Asks) != 1 || listResp.Asks[0].ID != id {
		t.Fatalf("asks=%+v", listResp.Asks)
	}

	// 提交回答。
	body := strings.NewReader(`{"answer":"alice"}`)
	req := httptest.NewRequest("POST", "/api/ask/"+id, body)
	req.SetPathValue("id", id)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.handleAnswerAsk(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("AskUser 错误: %v", res.err)
		}
		if res.ans != "alice" {
			t.Fatalf("回答=%q 期望 alice", res.ans)
		}
	case <-time.After(time.Second):
		t.Fatal("AskUser 未返回")
	}
}

// TestAnswerUnknownAsk 验证回答不存在的问题返回 404。
func TestAnswerUnknownAsk(t *testing.T) {
	h, _, _ := newTestHandler(t)
	req := httptest.NewRequest("POST", "/api/ask/nope", strings.NewReader(`{"answer":"x"}`))
	req.SetPathValue("id", "nope")
	rec := httptest.NewRecorder()
	h.handleAnswerAsk(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// TestStateUpdates 验证 agent 事件会更新 /api/state 的状态。
func TestStateUpdates(t *testing.T) {
	h, bus, _ := newTestHandler(t)
	bus.Publish(eventbus.Event{Type: eventbus.EventAgentStart, Agent: "listener"})
	bus.Publish(eventbus.Event{Type: eventbus.EventAgentDone, Agent: "listener"})
	// 给状态监听 goroutine 一点时间处理。
	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest("GET", "/api/state", nil)
	rec := httptest.NewRecorder()
	h.handleState(rec, req)
	var st flowState
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Stage != "listener" {
		t.Fatalf("stage=%s", st.Stage)
	}
	if len(st.Agents) != 9 {
		t.Fatalf("agents=%d", len(st.Agents))
	}
	if st.Agents[0].Status != "done" {
		t.Fatalf("listener status=%s", st.Agents[0].Status)
	}
}

// TestSSE 验证 SSE /api/events 推送 bus 事件。
func TestSSE(t *testing.T) {
	h, bus, _ := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.handleEvents(rec, req)
		close(done)
	}()

	// 等待 SSE 订阅就绪后发布事件。
	time.Sleep(150 * time.Millisecond)
	bus.Publish(eventbus.Event{Type: eventbus.EventLog, Agent: "test", Data: "hello-sse"})
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Fatalf("SSE 缺少 data 前缀: %s", body)
	}
	if !strings.Contains(body, "hello-sse") {
		t.Fatalf("SSE 未推送事件内容: %s", body)
	}
	if !strings.Contains(body, "log") {
		t.Fatalf("SSE 未推送事件类型: %s", body)
	}
}

// TestRegisterRoutes 验证 Register 注册的路由可通过真实 mux 访问。
func TestRegisterRoutes(t *testing.T) {
	h, _, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// /api/state
	resp, err := http.Get(srv.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("state status=%d", resp.StatusCode)
	}
	var st flowState
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(st.Agents) != 9 {
		t.Fatalf("agents=%d 期望 9", len(st.Agents))
	}

	// /api/config
	resp2, err := http.Get(srv.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("config status=%d", resp2.StatusCode)
	}
	resp2.Body.Close()

	// / 首页
	resp3, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("index status=%d", resp3.StatusCode)
	}
	ct := resp3.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("index content-type=%s", ct)
	}
	resp3.Body.Close()
}

// TestGithubAndConfig 验证 GitHub 配置写入与 /api/config 脱敏读取。
func TestGithubAndConfig(t *testing.T) {
	h, _, _ := newTestHandler(t)
	body := strings.NewReader(`{"remote":"https://github.com/x/y","branch":"main","token":"ghp_secret"}`)
	req := httptest.NewRequest("POST", "/api/github", body)
	rec := httptest.NewRecorder()
	h.handleGithub(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("github status=%d", rec.Code)
	}

	// 读取配置，token 应被脱敏。
	req2 := httptest.NewRequest("GET", "/api/config", nil)
	rec2 := httptest.NewRecorder()
	h.handleConfig(rec2, req2)
	var cfg map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	gh, _ := cfg["github"].(map[string]any)
	if gh["remote"] != "https://github.com/x/y" {
		t.Fatalf("remote=%v", gh["remote"])
	}
	if gh["token"] != "***" {
		t.Fatalf("token 未脱敏: %v", gh["token"])
	}
}
