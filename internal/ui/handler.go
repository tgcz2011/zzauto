package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tgcz2011/zzauto/internal/config"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// agentDef 描述单个 agent 的阶段标识与展示名称。
type agentDef struct {
	Stage string
	Name  string
}

// agentOrder 编排流程中 9 个 agent 的顺序与中文名。
var agentOrder = []agentDef{
	{workspace.StageListener, "Listener 倾听者"},
	{workspace.StageAsker, "Asker 询问者"},
	{workspace.StagePlanner, "Planner 规划者"},
	{workspace.StageDesigner, "Designer 设计者"},
	{workspace.StageEvaluator, "Evaluator 评估者"},
	{workspace.StageManager, "Manager 管理者"},
	{workspace.StageExecutor, "Executor 执行者"},
	{workspace.StageGenerator, "Generator 生成者"},
	{workspace.StageGittor, "Gittor 提交者"},
}

// docNameMap 将短名映射到工作区文档文件名。
var docNameMap = map[string]string{
	"desire": workspace.DocDesire,
	"need":   workspace.DocNeed,
	"spec":   workspace.DocSpec,
	"deal":   workspace.DocDeal,
	"task":   workspace.DocTask,
}

// askTimeout 等待用户回答的最长时间。
const askTimeout = 10 * time.Minute

// askEntry 一条待回答的问题及其回复 channel。
// 同时承载问题文本与创建时间，供 /api/asks 列出。
type askEntry struct {
	id        string
	question  string
	createdAt time.Time
	ch        chan string
}

// agentState 单个 agent 的运行状态（用于 /api/state）。
type agentState struct {
	Stage  string `json:"stage"`
	Name   string `json:"name"`
	Status string `json:"status"` // pending / running / done / failed
}

// flowState 整体流程状态（用于 /api/state）。
type flowState struct {
	Stage     string       `json:"stage"`
	Agents    []agentState `json:"agents"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// docMetaView 文档元信息的 JSON 视图（小写键，供前端使用）。
type docMetaView struct {
	Stage     string    `json:"stage"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// askView 一条待答问题的 JSON 视图（用于 /api/asks）。
type askView struct {
	ID        string    `json:"id"`
	Question  string    `json:"question"`
	CreatedAt time.Time `json:"created_at"`
}

// Handler 是 Web UI 的 HTTP 处理器，持有事件总线、工作区与配置。
//
// 通过订阅 bus 维护流程状态，提供 REST API 与 SSE 推送，
// 并暴露 AskUser 方法作为 Asker agent 与浏览器交互的桥。
type Handler struct {
	bus *eventbus.Bus
	ws  *workspace.Workspace
	cfg *config.Config

	// mu 保护 asks 与 askSeq。
	mu     sync.Mutex
	asks   map[string]*askEntry
	askSeq int

	// stateMu 保护 state。
	stateMu sync.Mutex
	state   flowState
}

// New 创建 Handler 并启动事件监听以维护流程状态。
func New(bus *eventbus.Bus, ws *workspace.Workspace, cfg *config.Config) *Handler {
	h := &Handler{
		bus:  bus,
		ws:   ws,
		cfg:  cfg,
		asks: make(map[string]*askEntry),
	}
	h.initState()
	h.startStateListener()
	return h
}

// initState 初始化 9 个 agent 为 pending 状态。
func (h *Handler) initState() {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	agents := make([]agentState, 0, len(agentOrder))
	for _, a := range agentOrder {
		agents = append(agents, agentState{Stage: a.Stage, Name: a.Name, Status: "pending"})
	}
	h.state = flowState{Stage: "", Agents: agents, UpdatedAt: time.Now()}
}

// startStateListener 订阅 bus，把事件转换为流程状态更新。
func (h *Handler) startStateListener() {
	ch := h.bus.Subscribe()
	go func() {
		for evt := range ch {
			h.applyEvent(evt)
		}
	}()
}

// applyEvent 根据事件更新流程状态。
func (h *Handler) applyEvent(evt eventbus.Event) {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	switch evt.Type {
	case eventbus.EventAgentStart:
		h.state.Stage = evt.Agent
		h.setAgentStatus(evt.Agent, "running")
	case eventbus.EventAgentDone:
		h.setAgentStatus(evt.Agent, "done")
	case eventbus.EventAgentFailed:
		h.state.Stage = evt.Agent
		h.setAgentStatus(evt.Agent, "failed")
	}
	h.state.UpdatedAt = evt.Time
}

// setAgentStatus 设置指定 agent 的状态。
func (h *Handler) setAgentStatus(stage, status string) {
	for i := range h.state.Agents {
		if h.state.Agents[i].Stage == stage {
			h.state.Agents[i].Status = status
			return
		}
	}
}

// Register 将全部 UI 路由注册到给定的 mux。
//
// 路由概览：
//   - GET  /                首页 index.html
//   - GET  /static/         静态资源（app.js / style.css）
//   - GET  /api/state       流程状态
//   - GET  /api/docs/{name} 读取文档（desire/need/spec/deal/task）
//   - POST /api/input       提交用户原始需求
//   - GET  /api/asks        待回答问题列表
//   - POST /api/ask/{id}    回答指定问题
//   - POST /api/github      配置 GitHub
//   - GET  /api/config      读取配置（token 脱敏）
//   - GET  /api/events      SSE 事件流
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /", h.handleIndex)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(assets()))))
	mux.HandleFunc("GET /api/state", h.handleState)
	mux.HandleFunc("GET /api/docs/{name}", h.handleGetDoc)
	mux.HandleFunc("POST /api/input", h.handleInput)
	mux.HandleFunc("GET /api/asks", h.handleListAsks)
	mux.HandleFunc("POST /api/ask/{id}", h.handleAnswerAsk)
	mux.HandleFunc("POST /api/github", h.handleGithub)
	mux.HandleFunc("GET /api/config", h.handleConfig)
	mux.HandleFunc("GET /api/events", h.handleEvents)
}

// AskUser 供 Asker agent 调用：发布问题并阻塞等待用户在 UI 上回答。
//
// 生成唯一 askID，发布 ask_user 事件，随后阻塞等待对应 channel 的回复，
// 超时（10 分钟）返回错误。这是 agent 与 UI 交互的桥。
func (h *Handler) AskUser(question string) (string, error) {
	id := h.genAskID()
	entry := &askEntry{
		id:        id,
		question:  question,
		createdAt: time.Now(),
		ch:        make(chan string, 1),
	}
	h.mu.Lock()
	h.asks[id] = entry
	h.mu.Unlock()

	h.bus.Publish(eventbus.Event{
		Type:  eventbus.EventAskUser,
		Agent: "asker",
		Data: map[string]any{
			"id":       id,
			"question": question,
		},
	})

	select {
	case ans := <-entry.ch:
		return ans, nil
	case <-time.After(askTimeout):
		h.mu.Lock()
		delete(h.asks, id)
		h.mu.Unlock()
		return "", fmt.Errorf("等待用户回答超时（%s）", askTimeout)
	}
}

// genAskID 生成唯一的问题 ID。
func (h *Handler) genAskID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.askSeq++
	return fmt.Sprintf("ask-%d-%d", time.Now().UnixNano(), h.askSeq)
}

// handleIndex 提供内嵌的 index.html。
func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := embeddedFiles.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "index.html 未找到", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

// handleState 返回当前流程状态。
func (h *Handler) handleState(w http.ResponseWriter, r *http.Request) {
	h.stateMu.Lock()
	st := h.state
	agents := make([]agentState, len(h.state.Agents))
	copy(agents, h.state.Agents)
	st.Agents = agents
	h.stateMu.Unlock()
	writeJSON(w, http.StatusOK, st)
}

// handleGetDoc 返回指定文档内容（含 frontmatter 解析）。
func (h *Handler) handleGetDoc(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	docFile, ok := docNameMap[name]
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "未知文档: " + name})
		return
	}
	raw, err := h.ws.ReadDoc(docFile)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "文档不存在", "name": name})
		return
	}
	meta, body := workspace.ParseDoc(raw)
	writeJSON(w, http.StatusOK, map[string]any{
		"name": name,
		"raw":  raw,
		"body": body,
		"meta": docMetaView{
			Stage:     meta.Stage,
			Status:    meta.Status,
			UpdatedAt: meta.UpdatedAt,
		},
	})
}

// handleInput 接收用户原始需求，写入 input.md 并附带 frontmatter。
func (h *Handler) handleInput(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Request string `json:"request"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体格式错误"})
		return
	}
	if strings.TrimSpace(body.Request) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request 不能为空"})
		return
	}
	content := workspace.RenderDoc(workspace.DocMeta{
		Stage:     workspace.StageListener,
		Status:    workspace.StatusPending,
		UpdatedAt: time.Now(),
	}, body.Request)
	if err := h.ws.WriteDoc("input.md", content); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	h.bus.Publish(eventbus.Event{
		Type:  eventbus.EventLog,
		Agent: "ui",
		Data:  "收到用户需求，已写入 input.md",
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleListAsks 返回当前待回答的问题列表。
func (h *Handler) handleListAsks(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	views := make([]askView, 0, len(h.asks))
	for id, e := range h.asks {
		views = append(views, askView{ID: id, Question: e.question, CreatedAt: e.createdAt})
	}
	h.mu.Unlock()
	sort.Slice(views, func(i, j int) bool { return views[i].CreatedAt.Before(views[j].CreatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"asks": views})
}

// handleAnswerAsk 接收用户对某问题的回答并投递给等待中的 AskUser。
func (h *Handler) handleAnswerAsk(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体格式错误"})
		return
	}
	h.mu.Lock()
	entry, ok := h.asks[id]
	if ok {
		delete(h.asks, id)
	}
	h.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "问题不存在或已回答"})
		return
	}
	// channel 缓冲为 1，AskUser 正在等待，投递不会阻塞。
	select {
	case entry.ch <- body.Answer:
	default:
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleGithub 更新 GitHub 配置。
//
// 持久化说明：受 UI 包仅使用标准库的约束（不引入 yaml 依赖），
// 此处仅在内存中更新 cfg；如需落盘 zzauto.yaml，应由 config 包
// 提供 Save 方法实现。
func (h *Handler) handleGithub(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Remote string `json:"remote"`
		Branch string `json:"branch"`
		Token  string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体格式错误"})
		return
	}
	h.cfg.Github.Remote = body.Remote
	h.cfg.Github.Branch = body.Branch
	h.cfg.Github.Token = body.Token
	h.bus.Publish(eventbus.Event{
		Type:  eventbus.EventLog,
		Agent: "ui",
		Data:  "GitHub 配置已更新",
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleConfig 返回当前配置（token 脱敏）。
func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	token := h.cfg.Github.Token
	masked := ""
	if token != "" {
		masked = "***"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"listen":        h.cfg.Listen,
		"aicli_addr":    h.cfg.AicliAddr,
		"workspace_dir": h.cfg.WorkspaceDir,
		"github": map[string]any{
			"remote": h.cfg.Github.Remote,
			"branch": h.cfg.Github.Branch,
			"token":  masked,
		},
	})
}

// handleEvents 是 SSE 端点：订阅 bus 并把事件推送给客户端。
//
// 客户端断连（请求上下文取消）时退出，Bus 当前未提供反订阅，
// 该订阅 channel 会被后续 Publish 非阻塞丢弃，不影响发布者。
func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	ch := h.bus.Subscribe()
	ctx := r.Context()
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(map[string]any{
				"type":  evt.Type,
				"agent": evt.Agent,
				"data":  evt.Data,
				"time":  evt.Time,
			})
			if err == nil {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
				if flusher != nil {
					flusher.Flush()
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

// writeJSON 写入 JSON 响应。
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
