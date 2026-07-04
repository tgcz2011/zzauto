# 5 分钟上手

本文带你用最短时间跑通 zzauto：安装 → 启动 → 提交一个需求 → 观察 9 agent 流程 → 配置 GitHub 等待自动提交。

> zzauto 是多层 agent 协作的 AI 自主编程平台，端到端把一句话需求变成可提交的代码。整体架构见 [architecture.md](./architecture.md)，9 个 agent 的职责见 [agents.md](./agents.md)。

---

## 1. 前置条件

- **操作系统**：macOS / Linux（amd64 或 arm64）、Windows（amd64）。
- **aiclibridge（本地 AI 网关）**：通常无需手动预装。zzauto `serve` 启动时会向 aiclibridge 做 5 秒健康检查，不可达则自动执行安装脚本（curl|sh / irm|iex）并轮询健康检查（最长 30s），安装成功后继续启动。
  - aiclibridge 是统一 AI 调用桥，zzauto 所有 agent 通过它调用 AI，避免直接耦合任何 AI SDK/CLI。详见 [aiclibridge.md](./aiclibridge.md)。
  - 如需跳过自动安装（例如离线环境或希望严格管控安装来源），可加 `--no-auto-install` flag，serve 仅提示安装命令并退出。
  - 也可手动预装，见下文「安装 aiclibridge」一节。
- **gh CLI（v0.3.0 起必装）**：serve 启动时检测 `gh` 是否在 PATH，未装则按平台打印安装命令并退出 1；未登录则提示 `gh auth login` 退出 1。详见下文「安装 gh CLI」与 [gh-integration.md](./gh-integration.md)。
- **git**：Gittor 提交代码依赖本地 `git` 命令，请确保已安装并在 `PATH` 中。
- **Go（可选）**：仅当你要从源码构建时需要 Go 1.26+，预编译二进制无需 Go。

---

## 2. 安装 zzauto

### macOS / Linux

一行命令安装（走 GitHub releases 直链，含 sha256 校验）：

```sh
curl -fsSL https://github.com/tgcz2011/zzauto/raw/main/scripts/install.sh | sh
```

可选参数（通过 `sh -s --` 透传）：

```sh
# 安装指定版本
curl -fsSL https://github.com/tgcz2011/zzauto/raw/main/scripts/install.sh | sh -s -- --version v0.1.0

# 指定安装路径
curl -fsSL https://github.com/tgcz2011/zzauto/raw/main/scripts/install.sh | sh -s -- --bin /usr/local/bin/zzauto

# 覆盖已存在的二进制
curl -fsSL https://github.com/tgcz2011/zzauto/raw/main/scripts/install.sh | sh -s -- --force

# 中国大陆加速（GitHub 镜像前缀）
GITHUB_MIRROR=https://ghproxy.com/ curl -fsSL https://github.com/tgcz2011/zzauto/raw/main/scripts/install.sh | sh
```

默认安装路径：优先 `/usr/local/bin/zzauto`（不可写时回退 `~/.local/bin/zzauto`）。若目录不在 `PATH`，脚本会提示你追加到 `~/.bashrc` 或 `~/.zshrc`。

### Windows（PowerShell）

```powershell
irm https://github.com/tgcz2011/zzauto/raw/main/scripts/install.ps1 | iex
```

默认安装到 `%USERPROFILE%\bin\zzauto.exe`。如需指定版本/路径，可先下载脚本再用会话变量运行：

```powershell
$InstallVersion = "v0.1.0"
$InstallBin     = "C:\bin\zzauto.exe"
irm https://github.com/tgcz2011/zzauto/raw/main/scripts/install.ps1 | iex
```

### 验证安装

```sh
zzauto version
# 输出: v0.3.0
```

---

## 3. 安装 gh CLI（必装，v0.3.0 起）

`serve` 启动时会校验 gh CLI 已安装并已登录，未装/未登录均退出 1。安装与登录步骤如下，详见 [gh-integration.md](./gh-integration.md)。

### macOS

```sh
# 任选其一
xcode-select --install      # 安装 Xcode Developer Tools（含 gh）
brew install gh
```

### Linux（按发行版）

```sh
sudo apt install gh              # Debian/Ubuntu
sudo dnf install gh              # Fedora/RHEL
sudo pacman -S github-cli        # Arch
```

### Windows（PowerShell）

```powershell
winget install GitHub.cli
# 或 choco install gh
```

### 登录 GitHub

```sh
gh auth login
# 按提示选择：GitHub.com → HTTPS → 浏览器登录或粘贴 Personal Access Token
```

验证登录状态：

```sh
gh auth status   # 输出 "Logged in to github.com as ..." 即成功
```

> gh 未装/未登录时 `serve` 退出码 1；UI 也可通过 `GET /api/gh/status` 查询状态、`GET /api/gh/repos` 拉取仓库列表（用于新建项目弹窗）。

---

## 4. 安装 aiclibridge（可选）

zzauto `serve` 启动时会自动处理 aiclibridge 的安装：

- 先做健康检查（默认地址 `127.0.0.1:8787`，5 秒超时）。
- 不可达时自动执行平台对应的安装脚本（macOS/Linux 走 `curl -fsSL ... | sh`，Windows 走 `irm ... | iex`），安装后每 2 秒轮询一次健康检查，最长等待 30 秒。
- 安装成功后继续启动 serve；若 30 秒仍未可达，则打印失败信息与手动安装命令并以退出码 1 退出。
- 加 `--no-auto-install` flag 可跳过自动安装，仅提示手动安装命令后退出。

### 手动预装（可选）

如你希望提前装好 aiclibridge（例如离线环境需先准备脚本，或想用自定义安装来源），可手动执行：

```sh
# macOS / Linux
curl -fsSL https://github.com/tgcz2011/aiclibridge/raw/main/scripts/install.sh | sh

# Windows (PowerShell)
irm https://github.com/tgcz2011/aiclibridge/raw/main/scripts/install.ps1 | iex
```

安装后启动 aiclibridge，使其监听 `127.0.0.1:8787`（默认地址）。如你的 aiclibridge 部署在其它地址或需要 api_key，请在 `zzauto.yaml` 中配置 `aicli_addr` / `aicli_key`，详见 [configuration.md](./configuration.md) 与 [aiclibridge.md](./aiclibridge.md)。

---

## 5. 启动 serve

```sh
zzauto serve
```

- 默认监听 `127.0.0.1:8788`（可用 `--listen` 覆盖，如 `zzauto serve --listen 0.0.0.0:8788`）。
- 启动顺序（v0.3.0）：加载 `zzauto.yaml`（不存在则用默认值）→ 创建项目 Registry → aiclibridge 健康检查（不可达则自动安装，除非 `--no-auto-install`）→ **gh CLI EnsureInstalled（未装则按平台打印安装命令并退出 1）→ gh CLI AuthStatus（未登录则提示 `gh auth login` 退出 1）** → 启动 HTTP 服务。
- 编排器不再启动时自动装配，改为在 UI 选中项目点「启动编排」时按需 `POST /api/projects/{id}/start` 装配 9 agent 并执行 `orch.Run`。
- 不带任何子命令（直接 `zzauto`）等同 `serve`。

成功启动后会看到：

```
zzauto v0.3.0 监听 127.0.0.1:8788
```

> `serve` 的全部 flag 与行为见 [cli.md](./cli.md)。

---

## 6. 浏览器提交一个需求

1. 打开 [http://127.0.0.1:8788](http://127.0.0.1:8788)。
2. 在输入框提交一个简单需求，例如：

   > 做一个命令行 todo app，支持增删改查与按优先级排序。

3. 该需求会被写入工作区的 `input.md`（带 frontmatter），并触发编排器开始 9 agent 流程。

### 观察 9 agent 流程与文档流转

页面通过 SSE 实时推送 agent 事件，你可以看到 9 个 agent 依次流转：

```
Listener → Asker → Planner → (Designer ↔ Evaluator 讨论) → Manager → Executor → (Generator → Evaluator 评估) → Gittor
```

文档随之在 `workspace/projects/<projectID>/` 下逐个生成：

| 阶段 | 产出文档 |
| --- | --- |
| Listener | `desire.md`（用户原始需求 + 改进点） |
| Asker | `need.md`（澄清后的需求清单 N1/N2…） |
| Planner | `spec.md`（Why / What Changes / Impact / ADDED Requirements） |
| Designer ↔ Evaluator | `deal.md`（完工协议 + 验收标准 D1/D2…） |
| Manager | `task.md`（任务清单 T1/T2…） |
| Executor | `agents/generator/instruction.md`（隔离指令） |
| Generator | `code/` 代码文件 + `reports/generator.md`（自评 report） |
| Evaluator | `spec.md` 中 `### Requirement:` 打勾为 `### [x] Requirement:` |
| Gittor | 将 `code/` 提交并推送到远端 |

完整流程与文档示例见 [workflow.md](./workflow.md)。

### 回答 Asker 的提问

Asker 会以「挑剔模式」向你提出 1–3 个澄清问题（边界情况、非功能需求、验收标准等）。问题会出现在 UI 的待回答列表中，回答后 Asker 继续推进。单次提问等待最长 10 分钟，超时则失败。

---

## 7. 配置 GitHub 仓库并等待自动提交

### 方式一：UI 配置

在 UI 的 GitHub 配置区填写：

- **Remote**：仓库地址，如 `git@github.com:yourname/yourrepo.git`（SSH）或 `https://github.com/yourname/yourrepo.git`（HTTPS）。
- **Branch**：目标分支，如 `main`。
- **Token**：使用 HTTPS 远程时填 GitHub Personal Access Token；使用 SSH 可留空。

提交后配置即时生效（仅更新内存配置，不落盘 `zzauto.yaml`）。

### 方式二：zzauto.yaml 配置

在工作目录创建 `zzauto.yaml`：

```yaml
github:
  remote: git@github.com:yourname/yourrepo.git
  branch: main
  token: ""
```

字段说明与环境变量覆盖见 [configuration.md](./configuration.md)。

### 等待自动提交

当 Evaluator 代码评估通过（`spec.md` 所有 Requirement 打勾）后，Gittor 会：

1. 复核 `spec.md` 中无未打勾的 `### Requirement:`；
2. 调用 git CLI 将 `code/` 目录暂存、提交，commit message 形如 `feat: 实现 <projectID> 项目代码`（遵循 conventional commits）；
3. 推送到远端。

> token 安全：使用 HTTPS 远程时，token 仅在 push 时拼入临时 URL（`https://x-access-token:<token>@...`），不写入 git config；所有 git 输出会脱敏 token。详见 [architecture.md](./architecture.md) 的「gittor 隔离层」。

---

## 下一步

- 深入了解架构：[architecture.md](./architecture.md)
- 逐个理解 9 个 agent：[agents.md](./agents.md)
- 端到端流程与文档示例：[workflow.md](./workflow.md)
- 完整配置参考：[configuration.md](./configuration.md)
- CLI 命令详解：[cli.md](./cli.md)
- 参与开发：[development.md](./development.md)
- 多项目管理：[multi-projects.md](./multi-projects.md)
- gh CLI 集成：[gh-integration.md](./gh-integration.md)
- Settings 设置：[settings.md](./settings.md)
- 统计面板：[stats.md](./stats.md)
- 任务面板：[task-panel.md](./task-panel.md)
