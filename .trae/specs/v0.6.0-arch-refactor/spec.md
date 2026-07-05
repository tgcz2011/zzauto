# Spec: v0.6.0 架构精简（9→5 角色）+ 控制机制 + 异步需求注入

## 背景

v0.5.0 的 9 角色架构存在：Executor/GittorAgent 不调 LLM 却占独立阶段；Evaluator 双模式耦合；Designer↔Evaluator 讨论循环耗 10 次 LLM 调用；Planning 链路过长（6 阶段才到代码）；无暂停/增量迭代能力。

## 目标

1. **架构精简**：9→5 LLM 角色（Analyst/Architect/Planner/Coder/Reviewer）+ 1 新角色（Mixor）+ 2 工具函数
2. **项目本地目录**：ProjectMeta.LocalDir，workspace 即本地目录，UI 文件浏览
3. **暂停/终止/继续**：orchestrator 控制通道，阶段边界安全停止
4. **每项目角色模型**：ProjectMeta.RoleModels，三级回退，中途修改下次生效
5. **异步需求注入**：方案 C 队列+检查点，Mixor 融合判定冲突

## 1. 架构精简

### 新角色映射

| 旧角色 | 新角色 | 变化 |
|---|---|---|
| Listener + Asker + Planner | **Analyst** 分析者 | 合并 3→1：读 input.md，按需提问（内部循环），产出 spec.md |
| Designer + Evaluator(讨论) | **Architect** 架构师 | 合并 2→1：读 spec.md，**批判性自评**，产出 deal.md。取消讨论循环 |
| Manager | **Planner** 规划者 | 重命名：读 spec+deal → task.md |
| Executor | （工具函数） | 降级：`buildCoderInstruction(ws)` 不调 LLM |
| Generator | **Coder** 编码者 | 重命名：读 instruction → code/ + report |
| Evaluator(代码) | **Reviewer** 审查者 | 拆出：只评代码，不评协议 |
| GittorAgent | （工具函数） | 降级：`commitAndPush(ws, git)` 不调 LLM |
| — | **Mixor** 融合者 | 新增：需求队列管理 + 文档/代码融合 + 进度报告 |

### 新文档流

```
input.md ─ Analyst ─▶ spec.md ─ Architect ─▶ deal.md ─ Planner ─▶ task.md
                                                                         │
                                              [buildCoderInstruction] ◀─┤
                                                                         │
                                              Coder ─▶ code/ + reports/coder.md
                                                                         │
                                              Reviewer ─▶ 通过/打回 Coder ─┤
                                                                         │
                                              [commitAndPush] ◀── 通过 ──┘
```

文档常量（workspace/doc.go）：
- 保留：DocInput="input.md"、DocSpec="spec.md"、DocDeal="deal.md"、DocTask="task.md"
- 新增：DocCoderReport="reports/coder.md"、DocReviewReport="reports/reviewer.md"、DocReqQueue="requirements_queue.md"、DocProgress="reports/progress.md"
- 删除：DocDesire、DocNeed（不再有独立 desire/need 文档，Analyst 直接产出 spec.md）

Stage 常量更新：
```go
StageAnalyst   = "analyst"
StageArchitect = "architect"
StagePlanner   = "planner"   // 注意：旧 planner 变成 analyst，旧 manager 变成 planner
StageCoder     = "coder"
StageReviewer  = "reviewer"
StageMixor     = "mixor"
```

### Architect 批判性思考要求

Architect 的 system prompt 必须包含：
```
你是架构师。你的职责是设计完工协议（deal.md），但在此之前你必须对 spec.md 进行批判性分析：

1. 需求完整性：spec 是否遗漏了边界情况、错误处理、安全、性能需求？
2. 技术可行性：spec 描述的方案是否技术上可行？是否有更简单的替代方案？
3. 一致性：spec 内部是否有矛盾？
4. 风险评估：哪些部分风险最高？需要怎样的验收标准？

你必须先写出批判性分析，再基于分析结果设计 deal.md。
deal.md 必须包含：设计决策、验收标准（D1/D2/...）、风险点与缓解措施。
```

### 新编排器流程

```go
func (o *Orchestrator) Run(ctx) error {
    // 1. Analyst（含内部提问循环）
    o.runStage(ctx, StageAnalyst)
    o.checkControl(ctx)  // 暂停/停止检查点
    
    // 2. Architect（单次，无讨论循环）
    o.runStage(ctx, StageArchitect)
    o.checkControl(ctx)
    
    // 3. Planner
    o.runStage(ctx, StagePlanner)
    o.checkControl(ctx)
    
    // 4. Coder → Reviewer 评估循环（最多 3 次）
    o.runEvalLoop(ctx)
    o.checkControl(ctx)
    
    // 5. commitAndPush（工具函数，非 agent）
    commitAndPush(o.ws, o.git, o.bus)
    
    // 6. 检查需求队列
    if hasQueuedRequirements(o.ws) {
        o.runMixor(ctx)
        // Mixor 决定：不冲突 → 从 Planner 继续；冲突 → 从 Analyst 重跑
    }
    return nil
}
```

### 工具函数

```go
// buildCoderInstruction 为 Coder 构造隔离指令文件（原 Executor 逻辑，不调 LLM）。
func buildCoderInstruction(ws *workspace.Workspace) error

// commitAndPush 提交并推送代码（原 GittorAgent 逻辑，不调 LLM）。
func commitAndPush(ws *workspace.Workspace, git agents.GittorClient, bus *eventbus.Bus) error
```

这两个函数在 orchestrator 包内实现，发 agent_start/agent_done 事件保持 UI 兼容，但不注册为 Agent 接口实现。

## 2. 项目本地目录

### ProjectMeta 变更

```go
type ProjectMeta struct {
    ID        string `json:"id"`
    Name      string `json:"name"`
    Repo      string `json:"repo"`
    Branch    string `json:"branch"`
    LocalDir  string            `json:"local_dir,omitempty"`  // 新增：本地目录路径
    CreatedAt int64  `json:"created_at"`
    UpdatedAt int64  `json:"updated_at"`
    Status    string `json:"status"`
    CurrentStage string         `json:"current_stage"`
    PausedStage  string         `json:"paused_stage,omitempty"`  // 新增：暂停阶段
    RoleModels   map[string]string `json:"role_models,omitempty"` // 新增：项目级模型
}
```

### Registry 变更

`Create` 方法签名变更，新增 localDir 参数：
```go
func (r *Registry) Create(name, repo, branch, localDir string) (*ProjectMeta, error)
```

如果 localDir 非空：
- 不创建 `projects/<id>/` 目录
- workspace.Path() 直接指向 localDir
- EnsureDirs 在 localDir 下创建 runs/、reports/、agents/ 等子目录

如果 localDir 为空：保持原逻辑（workspaceDir/projects/<id>）。

### API

```
POST /api/projects  body: {name, repo, branch, local_dir}
```

### UI

项目页新建弹窗增加"本地目录"字段（可选）。
项目详情区新增"文件浏览"面板：GET /api/projects/{id}/files?path=&depth= 返回文件树。

## 3. 暂停/终止/继续

### Orchestrator 控制通道

```go
type Orchestrator struct {
    // ... 现有字段
    controlMu   sync.Mutex
    controlSig  controlSignal  // none/pause/stop
    pausedCond  *sync.Cond     // 暂停时等待 resume
    paused      bool
}

type controlSignal int
const (
    sigNone  controlSignal = iota
    sigPause
    sigStop
)
```

### checkControl 在每个阶段边界调用

```go
func (o *Orchestrator) checkControl(ctx context.Context) error {
    o.controlMu.Lock()
    defer o.controlMu.Unlock()
    
    if o.controlSig == sigStop {
        return ErrStopped  // 安全停止
    }
    if o.controlSig == sigPause {
        o.paused = true
        // 发布暂停事件
        o.bus.Publish(eventbus.Event{Type: EventOrchPaused, ...})
        // 等待 resume
        o.pausedCond.Wait()
        o.paused = false
        o.controlSig = sigNone
        // 发布恢复事件
        o.bus.Publish(eventbus.Event{Type: EventOrchResumed, ...})
    }
    return nil
}
```

### API

```
POST /api/projects/{id}/pause    → 信号 sigPause
POST /api/projects/{id}/stop     → 信号 sigStop
POST /api/projects/{id}/resume   → 唤醒 pausedCond
```

Handler 的 handleStartProject 启动 orch 后，把 orchEntry 存入 map，控制 API 通过 orchEntry 找到 orch 发信号。

### 暂停状态持久化

暂停时把 PausedStage 写入 project.json。daemon 重启后若 PausedStage 非空，UI 显示"已暂停于 X 阶段"，用户点 resume 重新装配 orch 从该阶段继续。

## 4. 每项目角色模型

### ModelResolver 实现

```go
// ProjectModelResolver 从项目配置实时读取模型（不缓存）。
type ProjectModelResolver struct {
    reg      *projects.Registry
    projectID string
    global   map[string]string  // cfg.RoleModels
}

func (r *ProjectModelResolver) ModelFor(stage string) string {
    // 1. 项目级
    if r.reg != nil {
        if meta, err := r.reg.Get(r.projectID); err == nil {
            if m, ok := meta.RoleModels[stage]; ok && m != "" {
                return m
            }
        }
    }
    // 2. 全局级
    if m, ok := r.global[stage]; ok && m != "" {
        return m
    }
    // 3. 空串 = aiclibridge 默认
    return ""
}
```

### Agent 调用时实时解析

每个 agent 的 Run 方法中：
```go
model := agents.ResolveModel(resolver, StageAnalyst)  // 实时读项目配置
text, runID, err := RunWithTracking(ctx, ws, bus, ai, "analyst", model, system, user)
```

Orchestrator 持有 resolver，每次 runStage 时传入当前 agent。agent 通过 `ModelResolver` 参数获取模型。

### Agent 接口变更

```go
type Agent interface {
    Name() string
    Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus, resolver ModelResolver) error
}
```

（新增 resolver 参数；所有 agent 实现适配）

### UI

项目详情区新增"项目模型配置"按钮，弹出面板（与全局 Settings 页相同的 9→5 角色 select 下拉，但写入项目级 RoleModels）。
显示"继承全局"选项（value="" 表示用全局）。

## 5. 异步需求注入（方案 C）

### 需求队列

项目目录下 `requirements_queue.md`：
```markdown
## Requirement 1 (2026-07-05 10:30)
新增深色模式切换

## Requirement 2 (2026-07-05 10:35)
修复登录页 CSS 错位
```

### API

```
POST /api/projects/{id}/requirement  body: {request: "..."}
→ 追加到 requirements_queue.md
→ 如果 orch 正在运行，设置 sigPause（让当前阶段跑完后暂停触发 Mixor）
→ 如果 orch 未运行，只入队不触发
```

### Mixor 角色实现

```go
type Mixor struct{}
func (m *Mixor) Name() string { return "mixor" }
func (m *Mixor) Run(ctx, ws, ai, git, bus, resolver) error {
    // 1. 读 requirements_queue.md
    // 2. 读当前所有文档（spec/deal/task）+ code/ 目录
    // 3. 调 LLM 分析：
    //    system: "你是融合者。分析新需求与现有产出是否冲突。"
    //    user: 新需求 + 现有 spec/deal/task 摘要
    //    输出 JSON: {conflict: bool, action: "merge"|"rerun", merged_spec: "...", reason: "..."}
    // 4. 如果不冲突（merge）：
    //    a. 把新需求合并进 spec.md 和 task.md
    //    b. 融合 code/ 中的 WIP 代码（如果有）
    //    c. 清空 requirements_queue.md
    //    d. 发布进度报告到 reports/progress.md
    //    e. 返回 nil（orchestrator 从 Planner 继续）
    // 5. 如果冲突（rerun）：
    //    a. 保留 code/ 中的代码作为参考
    //    b. 清空 requirements_queue.md（需求已并入 input.md）
    //    c. 返回 ErrNeedRerun（orchestrator 从 Analyst 重跑）
}
```

### Orchestrator 集成

```go
func (o *Orchestrator) Run(ctx) error {
    for {
        if err := o.runPipeline(ctx); err != nil {
            return err
        }
        // 检查需求队列
        if !hasQueuedRequirements(o.ws) {
            return nil  // 无新需求，完成
        }
        // 有新需求，运行 Mixor
        err := o.runStage(ctx, StageMixor)
        if err == nil {
            // 不冲突，从 Planner 继续
            o.resumeFrom = StagePlanner
            continue  // 重新跑 runPipeline 但从 Planner 开始
        }
        if err == ErrNeedRerun {
            // 冲突，从 Analyst 重跑
            o.resumeFrom = StageAnalyst
            continue
        }
        return err
    }
}
```

### resumeFrom 支持

Orchestrator 新增 `resumeFrom string` 字段。runPipeline 根据 resumeFrom 跳过已完成的阶段：
```go
func (o *Orchestrator) runPipeline(ctx) error {
    stages := []string{StageAnalyst, StageArchitect, StagePlanner}
    startIdx := 0
    if o.resumeFrom != "" {
        startIdx = indexOf(stages, o.resumeFrom)
    }
    for i := startIdx; i < len(stages); i++ {
        o.runStage(ctx, stages[i])
        o.checkControl(ctx)
    }
    o.runEvalLoop(ctx)
    o.checkControl(ctx)
    commitAndPush(o.ws, o.git, o.bus)
    return nil
}
```

## 实施顺序

1. **架构重构**（agents + orchestrator + workspace）—— 基础，其他都依赖
2. **功能 3（每项目模型）**—— 依赖新 agent 接口（resolver 参数）
3. **功能 2（暂停/终止）**—— 依赖新 orchestrator
4. **功能 1（本地目录）**—— 独立，可并行
5. **功能 4（异步需求 + Mixor）**—— 依赖新 orchestrator + 控制机制
6. **UI 适配**—— 依赖后端 API 全部就绪
7. **测试 + 文档 + 发布**

## 验证

- `go build/vet/test ./...` 全通过
- 现有 e2e 测试适配新 5 角色流程
- 新增测试：Mixor 融合、暂停/恢复、项目级模型、本地目录
