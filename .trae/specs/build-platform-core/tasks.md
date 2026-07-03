# Tasks

- [x] Task 1: 初始化项目骨架与 Go 模块
  - [ ] SubTask 1.1: `go mod init github.com/tgcz2011/zzauto`，建立目录结构（cmd/zzauto, internal/orchestrator, internal/agents, internal/gittor, internal/aicli, internal/ui, web/, scripts/）
  - [ ] SubTask 1.2: 主入口 cmd/zzauto/main.go，CLI 子命令骨架（serve / uninstall / upgrade / version）
  - [ ] SubTask 1.3: 配置加载（zzauto.yaml + env，参考 aiclibridge 风格）

- [x] Task 2: 定义文档协议与 workspace 目录结构
  - [ ] SubTask 2.1: 定义 workspace 结构 `projects/<id>/{desire.md,need.md,spec.md,deal.md,task.md,agents/<name>/,report/}`
  - [ ] SubTask 2.2: 文档 frontmatter 状态字段（stage、status、updated_at）
  - [ ] SubTask 2.3: task.md 勾选语法与 spec.md 打勾约定

- [x] Task 3: aiclibridge 客户端封装
  - [ ] SubTask 3.1: internal/aicli/client.go：HTTP 调用 /v1/chat/completions 与 /v1/messages（含流式）
  - [ ] SubTask 3.2: 配置：地址、端口、api_key（env AICLIBRIDGE_* + yaml）
  - [ ] SubTask 3.3: 启动健康检查 /healthz，缺失时提示安装命令并退出或等待

- [x] Task 4: Agent 接口与编排器骨架
  - [ ] SubTask 4.1: internal/agents/agent.go：Agent 接口（Name / Run(ctx, workspace) error）
  - [ ] SubTask 4.2: internal/orchestrator/orchestrator.go：流程状态机（Listener→Asker→Planner→Designer↔Evaluator→Manager→Executor→Generator→Evaluator→Gittor）
  - [ ] SubTask 4.3: 事件总线（agent 状态变更、文档更新通知 UI）

- [x] Task 5: Listener Agent 实现
  - [ ] SubTask 5.1: 读取用户原始需求，调用 aiclibridge 丰富 prompt（补充改进点）
  - [ ] SubTask 5.2: 输出 desire.md（原始需求 + 改进点分点）

- [x] Task 6: Asker Agent 实现
  - [ ] SubTask 6.1: 读取 desire.md，生成待问问题列表
  - [ ] SubTask 6.2: 通过事件/UI 多轮提问，收集用户回答
  - [ ] SubTask 6.3: 挑剔模式判定：未满足时持续提问，满足后写 need.md（分点）

- [x] Task 7: Planner Agent 实现
  - [ ] SubTask 7.1: 读取 need.md，生成标准 spec.md（Why/What/Impact/Requirements）

- [x] Task 8: Designer + Evaluator 讨论实现
  - [ ] SubTask 8.1: Designer 读取 spec.md 起草 deal 草案
  - [ ] SubTask 8.2: Evaluator 批判性反驳（多轮，直到共识）
  - [ ] SubTask 8.3: 输出 deal.md（验收标准清单）

- [x] Task 9: Manager Agent 实现
  - [ ] SubTask 9.1: 读取 desire/need/spec/deal.md，生成可勾选 task.md（每项 id + 验收点）

- [x] Task 10: Executor + Generator 隔离执行
  - [ ] SubTask 10.1: Executor 按 task.md 为每个 Generator 创建隔离目录 + 指令文件（仅含任务描述/输出路径，无其他文档）
  - [ ] SubTask 10.2: Generator 仅读指令，调用 aiclibridge 写代码 + report
  - [ ] SubTask 10.3: 系统询问 Generator"你觉得合格了吗？"，获取确认后交付 report

- [x] Task 11: Evaluator 评估实现
  - [ ] SubTask 11.1: 对照 deal.md + spec.md + task.md + report 评估
  - [ ] SubTask 11.2: 合格：在 spec.md 打勾，转交 Gittor
  - [ ] SubTask 11.3: 不合格：生成问题清单反馈 Executor，回到 Task 10

- [x] Task 12: Gittor 隔离层实现
  - [ ] SubTask 12.1: internal/gittor/gittor.go：git add/commit/push（conventional commits）
  - [ ] SubTask 12.2: IPC 接口（HTTP /channel），其他 agent 不直接接触 git
  - [ ] SubTask 12.3: 仓库配置（remote url、branch、token），身份用 git CLI（非 gh api）

- [x] Task 13: Web UI 实现
  - [ ] SubTask 13.1: web/ 静态资源（Alpine.js + Tailwind，CDN 或本地 vendored）
  - [ ] SubTask 13.2: go:embed 将静态资源 embed 进二进制
  - [ ] SubTask 13.3: 页面：流程概览、agent 状态、文档查看、Asker 问答、GitHub 配置
  - [ ] SubTask 13.4: SSE/WebSocket 推送 agent 状态与文档更新

- [x] Task 14: 多平台安装脚本
  - [ ] SubTask 14.1: scripts/install.sh（curl | sh，探测 GOOS/GOARCH，sha256 校验，装 PATH）
  - [ ] SubTask 14.2: scripts/install.ps1（Windows PowerShell）
  - [ ] SubTask 14.3: uninstall 子命令（移除二进制与配置，保留项目数据）
  - [ ] SubTask 14.4: upgrade 子命令（GitHub releases 直链，绕开 gh api，sha256 校验）

- [x] Task 15: 端到端集成与冒烟测试
  - [ ] SubTask 15.1: 用一个简单需求（如 todo app）跑通全流程
  - [ ] SubTask 15.2: 验证文档流转、Generator 隔离、Gittor 提交到测试仓库
  - [ ] SubTask 15.3: 验证 uninstall / upgrade 命令

# Task Dependencies
- Task 3 depends on Task 1
- Task 4 depends on Task 1, Task 2
- Task 5 depends on Task 4, Task 3
- Task 6 depends on Task 5
- Task 7 depends on Task 6
- Task 8 depends on Task 7
- Task 9 depends on Task 8
- Task 10 depends on Task 9
- Task 11 depends on Task 10
- Task 12 depends on Task 1
- Task 13 depends on Task 4
- Task 14 depends on Task 1
- Task 15 depends on all
