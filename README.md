# ZZAuto

> 多层 agent 协作的 AI 自主编程平台，让 AI 像「团队」一样自主完成软件项目。

![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-GPL--3.0--or--later-blue.svg)
![Release](https://img.shields.io/badge/release-v0.3.0-orange)

---

## 项目介绍

**ZZAuto 是多层 agent 协作的 AI 自主编程平台**：把一次软件交付拆成 9 个固定角色（Listener、Asker、Planner、Designer、Evaluator、Manager、Executor、Generator、Gittor），按文档驱动的流程顺序协作，端到端地把用户一句话需求变成可提交的代码。

与单 agent「一把梭」编程不同，ZZAuto 把不同阶段隔离开来：Listener 只听需求、Asker 只问问题、Designer 只设计契约、Evaluator 只做批判、Generator 只读指令写代码、Gittor 只负责规范提交。每个 agent 都有清晰的输入文档与产出文档，互不污染上下文。

核心价值：

- **端到端自主**：从需求听取、澄清、规划、设计、评估、任务拆解、代码生成到 git 提交，全流程自动。
- **上下文隔离**：Generator 只能读 Executor 投喂的指令，看不到用户原始欲望与讨论过程，避免「越权发挥」。
- **文档驱动**：desire → need → spec → deal → task → report → 评估 → 提交，文档即协议、即进度、即审计。
- **规范提交**：Gittor 通过独立 git CLI 隔离层提交，遵循 conventional commits，不污染其他 agent。

## 特性

- **9 agent 分工协作**
  - `Listener` 听取用户原始需求，补充工程改进点，产出 `desire.md`
  - `Asker` 基于欲望挑剔提问，澄清边界与非功能需求，产出 `need.md`
  - `Planner` 规划规格（Why / What Changes / Impact / Requirements），产出 `spec.md`
  - `Designer` 设计完工协议与验收标准，产出 `deal.md`
  - `Evaluator` 批判性评估（讨论模式评协议、代码模式评实现），可多轮循环
  - `Manager` 读取上游四份文档拆解可勾选任务清单，产出 `task.md`
  - `Executor` 准备 Generator 指令文件（仅含任务正文 + spec 要点），保证上下文隔离
  - `Generator` 在隔离目录仅读指令写代码，自评后交付 `report`
  - `Gittor` 评估通过后将 `code/` 提交并推送到远端，遵循 conventional commits
- **文档驱动**：`desire.md → need.md → spec.md → deal.md → task.md → report`，agent 间通过 workspace 文件系统传递
- **Generator 上下文隔离**：只读 Executor 投喂的指令，不读 desire/need/spec/deal/task
- **aiclibridge 统一 AI 调用**：本地 HTTP 服务统一封装各 AI 后端；`serve` 时自动检测并安装，`upgrade` 时同步升级
- **Gittor 隔离层**：通过注入的 `GittorClient` 调用 git CLI，conventional commits，不污染其他 agent
- **Web UI**：embed 进单二进制，SSE 实时推送 agent 事件，Asker 交互问答通过浏览器完成
- **多项目支持**：`internal/projects` Registry 管理多项目，UI 项目列表/新建/切换/删除，每个项目独立 workspace 与 `project.json` 元数据
- **gh CLI 集成**：`serve` 启动时检测 gh CLI，未装则按平台打印安装命令并退出 1，未登录则提示 `gh auth login` 退出 1；新建项目弹窗从 `gh repo list` 拉取仓库下拉
- **创建项目选 gh 仓库**：UI 新建项目弹窗从 `GET /api/gh/repos` 拉取仓库列表，选择 owner/repo 与分支即完成项目与远端仓库绑定
- **每角色模型配置**：`config.RoleModels` + Settings 页，9 个 agent 各自配置模型，env `ZZAUTO_ROLE_MODEL_<STAGE>` 覆盖，持久化到 `zzauto.yaml`
- **统计面板**：从 aiclibridge `/v1/stats/usage|summary|concurrency` 拉取 token/USD/并发数据，4 卡片 + 模型分布表 + 自动刷新
- **任务面板**：每个 agent 的 run 详情（thinking/text/tool_use/tool_result/result/error 事件流），按项目+agent 查看，SSE 实时推送
- **多平台安装**：macOS/Linux 一行 `curl | sh`，Windows 一行 `irm | iex`；`uninstall` / `upgrade` 走 GitHub releases 直链，不依赖 `gh` CLI

## 架构图

```
                      ┌──────────────────────────────────────────┐
                      │              zzauto serve                │
                      │  (HTTP :8788 + Web UI + Event Bus SSE)   │
                      └─────────────────────┬────────────────────┘
                                            │ 提交需求
                                            ▼
   ┌─────────────────────────────────────────────────────────────────────┐
   │                       9 Agent 编排流程                                │
   │                                                                     │
   │   Listener ──▶ Asker ──▶ Planner ──┐                                │
   │     │           │          │       │ 讨论循环（最多 5 轮）          │
   │     │           │          │       ▼                                │
   │     │           │          │   Designer ◀──▶ Evaluator             │
   │     │           │          │     起草/修订     批判/共识            │
   │     │           │          │       │                               │
   │     │           │          │       ▼ 共识                          │
   │     │           │          │     Manager                          │
   │     │           │          │       │ 拆解任务                       │
   │     │           │          │       ▼                               │
   │     │           │          │     Executor ── 准备隔离指令           │
   │     │           │          │       │                               │
   │     │           │          │       ▼ 评估循环（最多 3 次）          │
   │     │           │          │     Generator ──▶ Evaluator           │
   │     │           │          │       生成/修复     评估/通过          │
   │     │           │          │       │                               │
   │     │           │          │       ▼ 通过                          │
   │     │           │          │     Gittor ── 提交 + 推送             │
   │     │           │          │                                       │
   └─────┴───────────┴──────────┴───────────────────────────────────────┘
```

文档流转：

```
desire.md ──▶ need.md ──▶ spec.md ──▶ deal.md ──▶ task.md ──▶ instruction.md
 (Listener)   (Asker)    (Planner)   (Designer)  (Manager)    (Executor)
                                                                  │
                                                                  ▼
                                                              code/ + report
                                                              (Generator)
                                                                  │
                                                                  ▼
                                                          spec.md 打勾 + 提交
                                                          (Evaluator + Gittor)
```

## 快速开始

### macOS / Linux

```sh
# 一键安装（走 GitHub releases 直链，含 sha256 校验）
curl -fsSL https://github.com/tgcz2011/zzauto/raw/main/scripts/install.sh | sh

# 安装指定版本
curl -fsSL https://github.com/tgcz2011/zzauto/raw/main/scripts/install.sh | sh -s -- --version v0.1.0

# 中国大陆加速
GITHUB_MIRROR=https://ghproxy.com/ curl -fsSL https://github.com/tgcz2011/zzauto/raw/main/scripts/install.sh | sh
```

安装 gh CLI（v0.3.0 起 serve 启动前置依赖）：

```sh
# macOS
brew install gh          # 或先 xcode-select --install
# Debian/Ubuntu
sudo apt install gh
# Fedora/RHEL
sudo dnf install gh
# Arch
sudo pacman -S github-cli

# 登录 GitHub（按提示选 GitHub.com → HTTPS → 浏览器登录或粘贴 token）
gh auth login
```

启动：

```sh
# 启动（首次会自动检测并安装 aiclibridge 本地 AI 网关，并校验 gh CLI 安装与登录状态）
zzauto serve

# 浏览器打开 http://127.0.0.1:8788，提交需求，等待 Asker 在页面问答
```

### Windows (PowerShell)

```powershell
# 一键安装（装到 %USERPROFILE%\bin\zzauto.exe）
irm https://github.com/tgcz2011/zzauto/raw/main/scripts/install.ps1 | iex

# 安装 gh CLI（v0.3.0 起 serve 启动前置依赖）
winget install GitHub.cli
# 或 choco install gh

# 登录
gh auth login

# 启动
zzauto serve

# 浏览器打开 http://127.0.0.1:8788
```

## 配置

ZZAuto 读取工作目录下的 `zzauto.yaml`，再用 `ZZAUTO_*` 环境变量覆盖。`serve` 启动时会先做 aiclibridge 健康检查。

### `zzauto.yaml` 示例

```yaml
# HTTP 监听地址
listen: 127.0.0.1:8788

# aiclibridge 本地 AI 网关
aicli_addr: 127.0.0.1:8787
aicli_key: ""

# 项目工作区根目录（每个项目在其下建子目录）
workspace_dir: ./workspace

# Git 远程仓库（Gittor 提交目标）
github:
  remote: git@github.com:tgcz2011/zzauto.git
  branch: main
  token: ""

# 每角色模型配置（key=stage 小写，value=模型名；空字符串走默认模型）
role_models:
  listener: ""
  asker: ""
  planner: ""
  designer: ""
  evaluator: ""
  manager: ""
  executor: ""
  generator: ""
  gittor: ""
```

### 环境变量覆盖

| 环境变量                    | 对应字段             |
| --------------------------- | -------------------- |
| `ZZAUTO_LISTEN`             | `listen`             |
| `ZZAUTO_AICLI_ADDR`         | `aicli_addr`         |
| `ZZAUTO_AICLI_KEY`          | `aicli_key`          |
| `ZZAUTO_WORKSPACE_DIR`      | `workspace_dir`      |
| `ZZAUTO_GITHUB_REMOTE`      | `github.remote`      |
| `ZZAUTO_GITHUB_BRANCH`      | `github.branch`      |
| `ZZAUTO_GITHUB_TOKEN`       | `github.token`       |
| `ZZAUTO_ROLE_MODEL_<STAGE>` | `role_models[stage]` |

## 命令参考

| 命令         | 说明                                         | Flags                                                                |
| ------------ | -------------------------------------------- | -------------------------------------------------------------------- |
| `serve`      | 启动 HTTP 服务与编排器（无子命令时的默认行为） | `--listen <addr>` 覆盖监听地址；`--no-auto-install` 跳过 aiclibridge 自动安装 |
| `uninstall`  | 移除二进制与配置（保留项目数据）             | —                                                                    |
| `upgrade`    | 从 GitHub releases 升级 zzauto 二进制         | —                                                                    |
| `version`    | 打印版本号                                   | —                                                                    |

> `serve` 启动顺序（v0.3.0）：加载配置 → aiclibridge 健康检查（不可达则自动安装或 `--no-auto-install` 退出 1）→ **gh CLI 检查（未装则按平台打印安装命令并退出 1，未登录则提示 `gh auth login` 退出 1）** → 启动 HTTP 服务。编排器不再启动时自动装配，改为 UI 在选中项目点「启动编排」时按需 `POST /api/projects/{id}/start`。

示例：

```sh
zzauto serve --listen 0.0.0.0:8788
zzauto serve --no-auto-install      # 已自行安装 aiclibridge 时跳过自动安装
zzauto upgrade
zzauto version
```

## 文档索引

- [快速开始](docs/quickstart.md)
- [架构设计](docs/architecture.md)
- [Agent 角色](docs/agents.md)
- [配置说明](docs/configuration.md)
- [CLI 命令](docs/cli.md)
- [aiclibridge 网关](docs/aiclibridge.md)
- [工作流程](docs/workflow.md)
- [开发指南](docs/development.md)
- [多项目管理](docs/multi-projects.md)
- [gh CLI 集成](docs/gh-integration.md)
- [Settings 设置](docs/settings.md)
- [统计面板](docs/stats.md)
- [任务面板](docs/task-panel.md)

## 开发

```sh
git clone https://github.com/tgcz2011/zzauto.git
cd zzauto
go build ./...
go test ./...
```

详细开发约定见 [docs/development.md](docs/development.md)。

## 贡献

欢迎提 Issue 与 PR。请先阅读 [CONTRIBUTING.md](CONTRIBUTING.md)。

**核心约定：每次代码修改都必须同步更新对应文档**（`docs/` 下相关文件、README、必要时还有 spec/deal/task）。文档与代码不一致是项目最常见的腐化来源，PR review 会把文档同步作为硬性门槛。

## Release 流程

版本号遵循 `vMAJOR.MINOR.PATCH`（如 `v0.1.0`），通过 git tag 触发 GitHub Actions 构建并发布 release 资产（macOS / Linux 的 `tar.gz` 与 Windows 的 `zip`，附带 `.sha256` 校验文件）。

详细步骤见 [RELEASE.md](RELEASE.md)。

## 许可证

本项目采用 [GPL-3.0-or-later](LICENSE) 协议发布。
