# ZZAuto AI 自主编程平台 Spec

## Why
当前 AI 辅助编程缺乏端到端的自主协作流程：从需求听取、澄清、设计、评估、执行到提交，各环节割裂，且单 agent 上下文容易污染。需要一个多层 agent 协作平台，让 AI 像「团队」一样自主完成软件项目，每个 agent 上下文隔离、职责单一，通过文档驱动协作，最终由隔离的 Gittor 完成规范提交。

## What Changes
- 新建 Go 后端编排器 `zzauto`，按固定流程调度 9 个 agent 角色（Listener/Asker/Planner/Designer/Evaluator/Manager/Executor/Generator/Gittor）
- 新建 Web UI（embed 进 Go 二进制），可视化流程状态、agent 输出、Asker 交互问答、GitHub 配置
- 集成 aiclibridge 作为唯一 AI 调用通道（HTTP API，不直接调任何 AI CLI/SDK）
- 新建 Gittor 隔离层，封装 git commit/push（用 git CLI，不用 gh api，避免频率限制）
- 新建多平台安装脚本：curl | sh（macOS/Linux）+ PowerShell（Windows），含 uninstall/upgrade（upgrade 走 GitHub releases 直链，绕开 gh api）
- 定义文档驱动协作协议：desire.md → need.md → spec.md → deal.md → task.md → report → 评估
- Generator 隔离工作目录，仅接收 Executor 指令，无法访问其他文档

## Impact
- Affected specs: 无（全新项目）
- Affected code: 全新仓库 `tgcz2011/zzauto`；外部依赖 aiclibridge（`tgcz2011/aiclibridge`，通过 HTTP 调用，不内联源码）
- 技术选型：Go 1.24+ 后端 + Web 前端（Alpine.js + Tailwind CSS）+ SQLite（可选，MVP 用文件系统）

## 技术选型理由
- **Go**：与 aiclibridge 同栈，单二进制部署，性能/内存最优，并发模型适合多 agent 编排
- **Web UI（embed）**：跨平台、易维护、美观（现代 CSS）、embed 保持单文件部署；比 Electron 轻（无需打包 Chromium），比 Flutter 无需额外 Dart 技能栈；后期如需桌面体验可 Electron 包装而不破坏 Web 架构
- **文件系统传递文档**：可观察、简单、符合「写进 desire.md」等显式需求，便于调试与人类介入
- **SQLite（可选）**：与 aiclibridge 一致，用于持久化 run 历史；MVP 可仅用文件系统

## ADDED Requirements

### Requirement: Agent 编排核心
系统 SHALL 提供一个 Go 编排器，按固定流程顺序调度 9 个 agent：Listener → Asker → Planner → Designer↔Evaluator → Manager → Executor → Generator(s) → Evaluator → Gittor。文档在 agent 间通过文件系统传递，编排器维护流程状态机。

#### Scenario: 端到端流程
- **WHEN** 用户提交需求并配置 GitHub 仓库
- **THEN** 编排器依次启动各 agent，文档按 desire.md → need.md → spec.md → deal.md → task.md → report 流转
- **AND** 最终由 Gittor 提交代码到指定仓库

#### Scenario: 流程可观察
- **WHEN** 任意 agent 状态变更
- **THEN** 编排器通过事件总线通知 UI 实时更新

### Requirement: Listener Agent
Listener SHALL 听取用户原始需求，自动补充改进点并丰富 prompt，将结果写入 `desire.md`（含用户原始需求 + Listener 补充的改进点，如可访问性、错误处理、边界情况等）。

#### Scenario: 丰富需求
- **WHEN** 用户输入"做一个 todo app"
- **THEN** Listener 输出 desire.md，包含原始需求与补充改进点

### Requirement: Asker Agent
Asker SHALL 基于 desire.md 向用户提问，挑剔地确认所有必要细节，直到认为需求已充分明确，将结果分点写入 `need.md`。

#### Scenario: 挑剔提问
- **WHEN** Asker 阅读 desire.md 后认为存在未明确点
- **THEN** Asker 通过 UI 向用户提问，用户回答后 Asker 继续判断是否还需提问
- **WHEN** Asker 认为所有必要事项均已明确
- **THEN** Asker 将分点结果写入 need.md 并通知编排器进入下一步

### Requirement: Planner Agent
Planner SHALL 根据 need.md 生成标准 `spec.md`（含 Why / What Changes / Impact / Requirements 结构）。

### Requirement: Designer + Evaluator 协议
Designer SHALL 根据 spec.md 起草完工协议，与 Evaluator 进行多轮批判性讨论（双方均质疑对方方案），直到达成共识，输出 `deal.md`。

#### Scenario: 批判性讨论
- **WHEN** Designer 提出验收标准草案
- **THEN** Evaluator 提出反对或补充意见
- **AND** 双方至少进行多轮交锋直至达成最终协议
- **AND** 最终 deal.md 记录双方认可的验收标准

### Requirement: Manager Agent
Manager SHALL 读取 desire.md / need.md / spec.md / deal.md，生成可勾选的 `task.md`（每项任务有唯一 id 与验收点）。

### Requirement: Executor Agent
Executor SHALL 根据 task.md / spec.md 将任务分配给 Generator(s)，每个 Generator 仅收到 Executor 的指令（含任务描述 + 输出路径），无法访问其他文档。

#### Scenario: 隔离分配
- **WHEN** Executor 分配任务给 Generator A
- **THEN** Generator A 的工作目录仅含 Executor 指令文件，不含 desire.md / need.md 等
- **AND** Generator A 完成后输出代码 + report 文本

### Requirement: Generator Agent
Generator SHALL 在隔离目录内仅按 Executor 指令写代码，完成后输出 report，并响应系统提问"你觉得你的项目合格了吗？"。

#### Scenario: 自评确认
- **WHEN** Generator 完成代码编写
- **THEN** 系统询问"你觉得你的项目合格了吗？"
- **WHEN** Generator 确认合格
- **THEN** Generator 输出 report 交付给 Evaluator

### Requirement: Evaluator Agent
Evaluator SHALL 根据 deal.md + spec.md + task.md + Generator 的 report 评估代码是否合格。合格则在 spec.md 对应项打勾并转交 Gittor；不合格则将问题反馈给 Executor 重新分配任务。

#### Scenario: 评估通过
- **WHEN** Evaluator 评估代码符合 deal.md 所有验收点
- **THEN** Evaluator 在 spec.md 打勾，将代码转交 Gittor

#### Scenario: 评估不通过
- **WHEN** Evaluator 发现代码不符合验收点
- **THEN** Evaluator 将问题清单反馈给 Executor，流程回到 Generator 执行步骤

### Requirement: Gittor Agent（GitHub 隔离层）
Gittor SHALL 封装所有 GitHub 相关操作（commit / push / branch），使用 git CLI 而非 gh api，确保其他 agent 上下文不被 git 操作污染。其他 agent 通过 IPC 请求 Gittor。

#### Scenario: 提交合格代码
- **WHEN** Evaluator 评估通过并将代码转交 Gittor
- **THEN** Gittor 在隔离环境执行 git add / commit / push 到用户配置的仓库
- **AND** commit message 遵循 conventional commits（feat / fix / docs / refactor 等）

#### Scenario: 上下文隔离
- **WHEN** 任意非 Gittor agent 运行
- **THEN** 该 agent 不直接调用 git，仅通过 IPC 请求 Gittor

### Requirement: aiclibridge 集成
系统 SHALL 通过 aiclibridge HTTP API 调用所有 AI，不直接调用任何 AI CLI/SDK。安装时 SHALL 检测 aiclibridge，缺失则提示安装命令。

#### Scenario: 统一 AI 调用
- **WHEN** 任意 agent 需要 AI 推理
- **THEN** 编排器通过 aiclibridge 的 `/v1/chat/completions` 或 `/v1/messages` 端点调用
- **AND** aiclibridge 地址 / 端口 / api_key 可配置（env + yaml）

#### Scenario: 缺失检测
- **WHEN** 系统启动且 aiclibridge 不可达
- **THEN** 提示用户安装命令（curl | sh）并退出或等待

### Requirement: 多平台安装脚本
系统 SHALL 提供 curl | sh（macOS/Linux）和 PowerShell（Windows）安装脚本，含 uninstall 与 upgrade 子命令。upgrade SHALL 走 GitHub releases 直链（绕开 gh api，避免频率限制）。

#### Scenario: 一键安装
- **WHEN** 用户执行 `curl -fsSL <url> | sh`
- **THEN** 脚本探测平台 / 架构、下载二进制、sha256 校验、安装到 PATH

#### Scenario: 卸载
- **WHEN** 用户执行 `zzauto uninstall`
- **THEN** 系统移除二进制与配置（保留用户项目数据）

#### Scenario: 升级
- **WHEN** 用户执行 `zzauto upgrade`
- **THEN** 系统从 GitHub releases 直链下载最新版并替换二进制（不调用 gh api）

### Requirement: UI 界面
系统 SHALL 提供 Web UI（embed 进 Go 二进制），展示流程状态、各 agent 输出、与 Asker 的交互问答、GitHub 配置。技术选型：Go 后端 + 轻量 Web 前端（HTML + Alpine.js + Tailwind CSS），MVP 用浏览器访问本地服务。

#### Scenario: 可视化流程
- **WHEN** 用户打开浏览器访问本地服务
- **THEN** UI 展示当前流程阶段、各 agent 状态、文档内容、Asker 待答问题

#### Scenario: 单文件部署
- **WHEN** 编译 zzauto
- **THEN** Web 静态资源 embed 进 Go 二进制，输出单一可执行文件

### Requirement: GitHub 仓库配置
系统 SHALL 在启动时让用户配置 GitHub 仓库（remote url、branch、可选 token），配置存于项目 workspace，供 Gittor 使用。

#### Scenario: 配置仓库
- **WHEN** 用户首次启动并输入需求
- **THEN** 系统提示配置 GitHub 仓库信息，Gittor 据此提交
