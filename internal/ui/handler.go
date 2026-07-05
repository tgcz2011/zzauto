package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tgcz2011/zzauto/internal/agents"
	"github.com/tgcz2011/zzauto/internal/aicli"
	"github.com/tgcz2011/zzauto/internal/config"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/ghcli"
	"github.com/tgcz2011/zzauto/internal/orchestrator"
	"github.com/tgcz2011/zzauto/internal/projects"
	"github.com/tgcz2011/zzauto/internal/registry"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// agentDef 描述单个 agent 的阶段标识与展示名称。
type agentDef struct {
	Stage string
	Name  string
}

// agentOrder 编排流程中 6 个 agent 的顺序与中文名（v0.6.0 精简）。
var agentOrder = []agentDef{
	{workspace.StageAnalyst, "Analyst 分析者"},
	{workspace.StageArchitect, "Architect 架构师"},
	{workspace.StagePlanner, "Planner 规划者"},
	{workspace.StageCoder, "Coder 编写者"},
	{workspace.StageReviewer, "Reviewer 审查者"},
	{workspace.StageMixor, "Mixor 融合者"},
}

// docNameMap 将短名映射到工作区文档文件名（v0.6.0 文档协议）。
var docNameMap = map[string]string{
	"input":      workspace.DocInput,
	"spec":       workspace.DocSpec,
	"deal":       workspace.DocDeal,
	"task":       workspace.DocTask,
	"queue":      workspace.DocReqQueue,
	"progress":   workspace.DocProgress,
	"coder":      workspace.DocCoderReport,
	"reviewer":   workspace.DocReviewReport,
}

// askTimeout 等待用户回答的最长时间。
const askTimeout = 10 * time.Minute

// askEntry 一条待回答的问题及其回复 channel。
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

// orchEntry 记录一个项目按需启动的 orchestrator 及其关联资源。
type orchEntry struct {
	orch     *orchestrator.Orchestrator
	ws       *workspace.Workspace
	cancel   context.CancelFunc
	resolver *ProjectModelResolver
}

// ProjectModelResolver 从项目配置实时读取模型（不缓存）。
//
// agents 包不能导入 projects 包（循环依赖），故 ProjectModelResolver
// 在 ui 包中实现（ui 包可同时导入 agents 和 projects）。
type ProjectModelResolver struct {
	Reg       *projects.Registry
	ProjectID string
	Global    map[string]string
}

// ModelFor 实现 agents.ModelResolver 接口。
// 优先使用项目级配置；未配置或为空则回退到全局配置；仍为空返回空串（AI 用默认模型）。
func (r *ProjectModelResolver) ModelFor(stage string) string {
	if r.Reg != nil {
		if meta, err := r.Reg.Get(r.ProjectID); err == nil {
			if m, ok := meta.RoleModels[stage]; ok && m != "" {
				return m
			}
		}
	}
	if r.Global != nil {
		if m, ok := r.Global[stage]; ok && m != "" {
			return m
		}
	}
	return ""
}

// Handler 是 Web UI 的 HTTP 处理器，持有事件总线、项目注册表与配置。
//
// 通过订阅 bus 维护流程状态，提供 REST API 与 SSE 推送，
// 并暴露 AskUser 方法作为 Analyst agent 与浏览器交互的桥。
type Handler struct {
	bus   *eventbus.Bus
	reg   *projects.Registry
	cfg   *config.Config
	aicli *aicli.Client // 用于 stats 代理

	// currentMu 保护 currentID（前端切换项目时更新）。
	currentMu sync.Mutex
	currentID string

	// mu 保护 asks 与 askSeq。
	mu     sync.Mutex
	asks   map[string]*askEntry
	askSeq int

	// stateMu 保护 state。
	stateMu sync.Mutex
	state   flowState

	// orchMu 保护 orchs（按需启动的 orchestrator 管理）。
	orchMu sync.Mutex
	orchs  map[string]*orchEntry // projectID -> 运行中的 orchestrator
}

// New 创建 Handler 并启动事件监听以维护流程状态。
func New(bus *eventbus.Bus, reg *projects.Registry, cfg *config.Config) *Handler {
	h := &Handler{
		bus:   bus,
		reg:   reg,
		cfg:   cfg,
		aicli: aicli.New(cfg.AicliAddr, cfg.AicliKey),
		asks:  make(map[string]*askEntry),
		orchs: make(map[string]*orchEntry),
	}
	h.initState()
	h.startStateListener()
	return h
}

// setCurrent 设置当前选中项目 ID。
func (h *Handler) setCurrent(id string) {
	h.currentMu.Lock()
	h.currentID = id
	h.currentMu.Unlock()
}

// currentWS 返回当前选中项目的 workspace；未选中时返回 nil。
func (h *Handler) currentWS() *workspace.Workspace {
	h.currentMu.Lock()
	id := h.currentID
	h.currentMu.Unlock()
	if id == "" {
		return nil
	}
	return workspace.NewFromProjectDir(h.reg.ProjectDir(id), id)
}

// initState 初始化 6 个 agent 为 pending 状态。
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

	// 项目管理
	mux.HandleFunc("GET /api/projects", h.handleListProjects)
	mux.HandleFunc("POST /api/projects", h.handleCreateProject)
	mux.HandleFunc("GET /api/projects/{id}", h.handleGetProject)
	mux.HandleFunc("DELETE /api/projects/{id}", h.handleDeleteProject)
	mux.HandleFunc("POST /api/projects/{id}/input", h.handleProjectInput)
	mux.HandleFunc("POST /api/projects/{id}/start", h.handleStartProject)
	mux.HandleFunc("POST /api/projects/{id}/select", h.handleSelectProject)

	// 暂停/停止/恢复
	mux.HandleFunc("POST /api/projects/{id}/pause", h.handlePauseProject)
	mux.HandleFunc("POST /api/projects/{id}/stop", h.handleStopProject)
	mux.HandleFunc("POST /api/projects/{id}/resume", h.handleResumeProject)

	// 异步需求
	mux.HandleFunc("POST /api/projects/{id}/requirement", h.handleAddRequirement)

	// 文件浏览
	mux.HandleFunc("GET /api/projects/{id}/files", h.handleListFiles)
	mux.HandleFunc("GET /api/projects/{id}/file", h.handleReadFile)

	// 项目级模型
	mux.HandleFunc("GET /api/projects/{id}/models", h.handleGetProjectModels)
	mux.HandleFunc("PUT /api/projects/{id}/models", h.handlePutProjectModels)

	// gh CLI
	mux.HandleFunc("GET /api/gh/status", h.handleGhStatus)
	mux.HandleFunc("GET /api/gh/repos", h.handleGhRepos)

	// settings
	mux.HandleFunc("GET /api/settings/models", h.handleGetModels)
	mux.HandleFunc("PUT /api/settings/models", h.handlePutModels)

	// aicli 模型列表（用于 Settings 页 model 下拉）
	mux.HandleFunc("GET /api/aicli/models", h.handleAicliModels)

	// stats 代理
	mux.HandleFunc("GET /api/stats/usage", h.handleStatsUsage)
	mux.HandleFunc("GET /api/stats/summary", h.handleStatsSummary)
	mux.HandleFunc("GET /api/stats/concurrency", h.handleStatsConcurrency)

	// runs
	mux.HandleFunc("GET /api/projects/{id}/runs", h.handleListRuns)
	mux.HandleFunc("GET /api/projects/{id}/runs/{rid}", h.handleGetRun)
}

// AskUser 供 Analyst agent 调用：发布问题并阻塞等待用户在 UI 上回答。
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
		Agent: "analyst",
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
	ws := h.currentWS()
	if ws == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "未选中项目"})
		return
	}
	raw, err := ws.ReadDoc(docFile)
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
	ws := h.currentWS()
	if ws == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "未选中项目"})
		return
	}
	content := workspace.RenderDoc(workspace.DocMeta{
		Stage:     workspace.StageAnalyst,
		Status:    workspace.StatusPending,
		UpdatedAt: time.Now(),
	}, body.Request)
	if err := ws.WriteDoc(workspace.DocInput, content); err != nil {
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
	select {
	case entry.ch <- body.Answer:
	default:
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleGithub 更新 GitHub 配置。
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

// ===== 项目管理 =====

// handleListProjects 返回全部项目列表与当前选中项目 ID。
func (h *Handler) handleListProjects(w http.ResponseWriter, r *http.Request) {
	metas, err := h.reg.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	h.currentMu.Lock()
	current := h.currentID
	h.currentMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"projects": metas,
		"current":  current,
	})
}

// handleCreateProject 创建新项目并自动选中。
func (h *Handler) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Repo     string `json:"repo"`
		Branch   string `json:"branch"`
		LocalDir string `json:"local_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体格式错误"})
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name 不能为空"})
		return
	}
	meta, err := h.reg.Create(body.Name, body.Repo, body.Branch, body.LocalDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	h.setCurrent(meta.ID)
	writeJSON(w, http.StatusOK, map[string]any{"project": meta})
}

// handleGetProject 返回单个项目元数据。
func (h *Handler) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	meta, err := h.reg.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目不存在", "id": id})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": meta})
}

// handleDeleteProject 删除项目。若为当前选中项目则清空选中。
func (h *Handler) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.reg.Delete(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	h.currentMu.Lock()
	if h.currentID == id {
		h.currentID = ""
	}
	h.currentMu.Unlock()
	// 同时停止该项目运行中的 orchestrator
	h.stopOrch(id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleProjectInput 写入指定项目的 input.md（带 frontmatter）。
func (h *Handler) handleProjectInput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.reg.Get(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目不存在", "id": id})
		return
	}
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
	ws := workspace.NewFromProjectDir(h.reg.ProjectDir(id), id)
	content := workspace.RenderDoc(workspace.DocMeta{
		Stage:     workspace.StageAnalyst,
		Status:    workspace.StatusPending,
		UpdatedAt: time.Now(),
	}, body.Request)
	if err := ws.WriteDoc(workspace.DocInput, content); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleStartProject 为指定项目按需装配 orchestrator 并启动。
// 若该项目已有运行中 orchestrator，返回 409 Conflict。
//
// workspace 目录使用 reg.ProjectDir(id)（支持 LocalDir 项目）。
// resolver 为 ProjectModelResolver，实时读取项目级模型覆盖。
func (h *Handler) handleStartProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	meta, err := h.reg.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目不存在", "id": id})
		return
	}
	h.orchMu.Lock()
	if _, exists := h.orchs[id]; exists {
		h.orchMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]any{"error": "该项目已有运行中的编排器"})
		return
	}
	h.orchMu.Unlock()

	ws := workspace.NewFromProjectDir(h.reg.ProjectDir(id), id)
	if err := ws.EnsureDirs(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	resolver := &ProjectModelResolver{
		Reg:       h.reg,
		ProjectID: id,
		Global:    h.cfg.RoleModels,
	}

	askFunc := agents.AskFunc(func(ctx context.Context, question string) (string, error) {
		return h.AskUser(question)
	})
	orch, err := registry.BuildOrchestrator(h.cfg, ws, h.bus, askFunc, resolver)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	// 若项目有 PausedStage，设置为 resumeFrom
	if meta.PausedStage != "" {
		// orchestrator 的 resumeFrom 是包内字段，无法直接设置；
		// 调用方重新启动时从头开始执行（暂停状态已清除）。
		_ = meta.PausedStage
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.orchMu.Lock()
	h.orchs[id] = &orchEntry{orch: orch, ws: ws, cancel: cancel, resolver: resolver}
	h.orchMu.Unlock()

	// 更新项目状态为 running
	meta.Status = workspace.StatusRunning
	_ = h.reg.Update(meta)

	go func() {
		if err := orch.Run(ctx); err != nil {
			h.bus.Publish(eventbus.Event{
				Type:  eventbus.EventLog,
				Agent: "orchestrator",
				Data:  fmt.Sprintf("项目 %s 编排器退出: %v", id, err),
			})
		}
		cancel()
		// 更新项目状态为 done
		if m, err := h.reg.Get(id); err == nil {
			m.Status = workspace.StatusDone
			_ = h.reg.Update(m)
		}
		h.orchMu.Lock()
		delete(h.orchs, id)
		h.orchMu.Unlock()
	}()

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleSelectProject 切换当前选中项目。
func (h *Handler) handleSelectProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.reg.Get(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目不存在", "id": id})
		return
	}
	h.setCurrent(id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "current": id})
}

// stopOrch 停止并移除指定项目的 orchestrator（若存在）。
func (h *Handler) stopOrch(id string) {
	h.orchMu.Lock()
	entry, ok := h.orchs[id]
	if ok {
		delete(h.orchs, id)
	}
	h.orchMu.Unlock()
	if ok {
		entry.orch.Stop()
		entry.cancel()
	}
}

// getOrchEntry 返回指定项目的运行中 orchestrator entry（不加锁的内部辅助）。
func (h *Handler) getOrchEntry(id string) *orchEntry {
	h.orchMu.Lock()
	defer h.orchMu.Unlock()
	return h.orchs[id]
}

// removeOrchEntry 移除指定项目的 orchestrator entry。
func (h *Handler) removeOrchEntry(id string) {
	h.orchMu.Lock()
	delete(h.orchs, id)
	h.orchMu.Unlock()
}

// handlePauseProject 暂停指定项目的 orchestrator。
func (h *Handler) handlePauseProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entry := h.getOrchEntry(id)
	if entry == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目未运行"})
		return
	}
	entry.orch.Pause()
	// 更新 project.json 的 PausedStage
	if meta, err := h.reg.Get(id); err == nil {
		meta.PausedStage = meta.CurrentStage
		_ = h.reg.Update(meta)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleStopProject 停止指定项目的 orchestrator。
func (h *Handler) handleStopProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entry := h.getOrchEntry(id)
	if entry == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目未运行"})
		return
	}
	entry.orch.Stop()
	entry.cancel()
	h.removeOrchEntry(id)
	// 更新项目状态
	if meta, err := h.reg.Get(id); err == nil {
		meta.Status = workspace.StatusFailed
		_ = h.reg.Update(meta)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleResumeProject 恢复已暂停的 orchestrator。
func (h *Handler) handleResumeProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entry := h.getOrchEntry(id)
	if entry == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目未运行，请重新启动"})
		return
	}
	entry.orch.Resume()
	// 清除 PausedStage
	if meta, err := h.reg.Get(id); err == nil {
		meta.PausedStage = ""
		_ = h.reg.Update(meta)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAddRequirement 异步追加需求到 requirements_queue.md。
// 若 orchestrator 正在运行，调 Pause() 让当前阶段完成后触发 Mixor。
func (h *Handler) handleAddRequirement(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.reg.Get(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目不存在", "id": id})
		return
	}
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
	ws := workspace.NewFromProjectDir(h.reg.ProjectDir(id), id)
	prev, _ := ws.ReadDoc(workspace.DocReqQueue)
	appended := strings.TrimRight(prev, "\n") + "\n" + strings.TrimRight(body.Request, "\n") + "\n"
	if err := ws.WriteDoc(workspace.DocReqQueue, appended); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	// 若 orchestrator 正在运行，请求暂停以触发 Mixor
	if entry := h.getOrchEntry(id); entry != nil {
		entry.orch.Pause()
		// 立即 Resume，让 orchestrator 在下个 checkControl 边界继续，
		// 跑完当前 pipeline 后 Run 会检查 requirements_queue。
		entry.orch.Resume()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// fileEntry 单个文件/目录信息（用于 /api/projects/{id}/files）。
type fileEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
	Path  string `json:"path"`
}

// handleListFiles 列出指定项目目录下的文件与子目录。
// query: path（相对路径，默认 ""）、depth（默认 2，目前仅支持 1 层）。
// 安全：禁止 path 包含 ".." 防止路径遍历。
func (h *Handler) handleListFiles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.reg.Get(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目不存在", "id": id})
		return
	}
	rel := r.URL.Query().Get("path")
	if rel == "" {
		rel = "."
	}
	// 安全：禁止 .. 防止路径遍历
	if strings.Contains(rel, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "path 不允许包含 .."})
		return
	}
	projectDir := h.reg.ProjectDir(id)
	target := filepath.Join(projectDir, rel)
	// 确保仍在 projectDir 之下
	absTarget, err := filepath.Abs(target)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	absRoot, _ := filepath.Abs(projectDir)
	if !strings.HasPrefix(absTarget, absRoot) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "path 超出项目目录"})
		return
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"files": []fileEntry{}})
		return
	}
	files := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileEntry{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  info.Size(),
			Path:  filepath.Join(rel, e.Name()),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// handleReadFile 读取项目内指定相对路径文件的内容（用于 UI 文件预览）。
// query: path（相对项目根，必填）。安全：禁止 ".." 且必须落在 projectDir 之下。
// 文件大于 256KB 时仅返回前 256KB 与截断标记，避免响应过大。
func (h *Handler) handleReadFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.reg.Get(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目不存在", "id": id})
		return
	}
	rel := r.URL.Query().Get("path")
	if rel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "path 必填"})
		return
	}
	if strings.Contains(rel, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "path 不允许包含 .."})
		return
	}
	projectDir := h.reg.ProjectDir(id)
	absRoot, _ := filepath.Abs(projectDir)
	target := filepath.Join(projectDir, rel)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if !strings.HasPrefix(absTarget, absRoot) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "path 超出项目目录"})
		return
	}
	info, err := os.Stat(absTarget)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "文件不存在", "path": rel})
		return
	}
	if info.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "path 是目录，不能读取", "path": rel})
		return
	}
	const maxPreview = 256 * 1024
	data, err := os.ReadFile(absTarget)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	truncated := false
	if len(data) > maxPreview {
		data = data[:maxPreview]
		truncated = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":      rel,
		"size":      info.Size(),
		"content":   string(data),
		"truncated": truncated,
	})
}

// handleGetProjectModels 返回项目级模型配置与全局配置。
func (h *Handler) handleGetProjectModels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	meta, err := h.reg.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目不存在", "id": id})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"models": meta.RoleModels,
		"global": h.cfg.RoleModels,
	})
}

// handlePutProjectModels 更新项目级模型配置。
// 无需重启 orchestrator——resolver 下次调用时实时读取。
func (h *Handler) handlePutProjectModels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	meta, err := h.reg.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目不存在", "id": id})
		return
	}
	var body struct {
		Models map[string]string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体格式错误"})
		return
	}
	meta.RoleModels = body.Models
	if err := h.reg.Update(meta); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ===== gh CLI =====

// handleGhStatus 返回 gh CLI 安装与登录状态。
func (h *Handler) handleGhStatus(w http.ResponseWriter, r *http.Request) {
	installed := true
	installHint := ""
	if err := ghcli.EnsureInstalled(); err != nil {
		installed = false
		installHint = err.Error()
	}
	loggedIn := false
	loginHint := ""
	if installed {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		ok, err := ghcli.AuthStatus(ctx)
		cancel()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		loggedIn = ok
		if !loggedIn {
			loginHint = ghcli.LoginHint()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installed":    installed,
		"logged_in":    loggedIn,
		"install_hint": installHint,
		"login_hint":   loginHint,
	})
}

// handleGhRepos 拉取当前登录用户的仓库列表。
func (h *Handler) handleGhRepos(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	repos, err := ghcli.Repos(ctx)
	if err != nil {
		if errors.Is(err, ghcli.ErrNotAuthenticated) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error":      err.Error(),
				"login_hint": ghcli.LoginHint(),
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

// ===== settings =====

// handleGetModels 返回当前角色模型配置与默认模型。
func (h *Handler) handleGetModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"models":  h.cfg.RoleModels,
		"default": aicli.DefaultModel,
	})
}

// handlePutModels 更新角色模型配置并落盘 zzauto.yaml。
func (h *Handler) handlePutModels(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Models map[string]string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体格式错误"})
		return
	}
	h.cfg.RoleModels = body.Models
	if err := h.cfg.Save("zzauto.yaml"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAicliModels 代理 aiclibridge /v1/models，供 Settings 页 model 下拉选择。
func (h *Handler) handleAicliModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.aicli.Models(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ===== stats 代理 =====

// handleStatsUsage 代理 aiclibridge /v1/stats/usage。
func (h *Handler) handleStatsUsage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	resp, err := h.aicli.Usage(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleStatsSummary 代理 aiclibridge /v1/stats/summary。
func (h *Handler) handleStatsSummary(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	resp, err := h.aicli.Summary(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleStatsConcurrency 代理 aiclibridge /v1/stats/concurrency。
func (h *Handler) handleStatsConcurrency(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	resp, err := h.aicli.Concurrency(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ===== runs =====

// runSummary 单个 run 的摘要信息（用于列表展示）。
type runSummary struct {
	ID        string `json:"id"`
	Agent     string `json:"agent"`
	File      string `json:"file"`
	CreatedAt int64  `json:"created_at"`
}

// handleListRuns 扫描指定项目 runs/*/*.json 返回 run 摘要列表。
func (h *Handler) handleListRuns(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.reg.Get(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目不存在", "id": id})
		return
	}
	runsDir := filepath.Join(h.reg.ProjectDir(id), "runs")
	agentDirs, err := os.ReadDir(runsDir)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"runs": []runSummary{}})
		return
	}
	runs := make([]runSummary, 0)
	for _, ad := range agentDirs {
		if !ad.IsDir() {
			continue
		}
		agent := ad.Name()
		pattern := filepath.Join(runsDir, agent, "*.json")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, m := range matches {
			fi, err := os.Stat(m)
			if err != nil {
				continue
			}
			runID := strings.TrimSuffix(filepath.Base(m), ".json")
			runs = append(runs, runSummary{
				ID:        runID,
				Agent:     agent,
				File:      filepath.Join("runs", agent, filepath.Base(m)),
				CreatedAt: fi.ModTime().Unix(),
			})
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt > runs[j].CreatedAt })
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

// handleGetRun 读取指定项目指定 run 的完整 JSON 内容。
func (h *Handler) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rid := r.PathValue("rid")
	if _, err := h.reg.Get(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "项目不存在", "id": id})
		return
	}
	pattern := filepath.Join(h.reg.ProjectDir(id), "runs", "*", rid+".json")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "run 不存在", "id": rid})
		return
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
