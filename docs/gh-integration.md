# gh CLI 集成

zzauto v0.3.0 起强制依赖 GitHub CLI（`gh`），用于启动检查、登录态校验、新建项目时的仓库下拉。本文说明 gh 的安装、登录、与 gittor 的分工，以及启动失败排查。

> 多项目与仓库选择见 [multi-projects.md](./multi-projects.md)；启动顺序总览见 [cli.md](./cli.md) 的 `serve` 行为段。

---

## 概述

`gh` 是 GitHub 官方命令行工具，提供 `gh auth login`、`gh repo list`、`gh repo view` 等子命令。zzauto 在 v0.3.0 引入对它的强依赖，原因：

- **统一登录入口**：用户只需 `gh auth login` 一次，zzauto 不再实现 token 输入 UI；
- **仓库下拉数据源**：新建项目时从 `gh repo list --json` 拉取当前账号仓库列表，避免手填错；
- **降低 token 落盘面**：zzauto 的 `github.token` 配置项仍保留用于 gittor push，但仓库列表读取不再要求额外 token。

`internal/ghcli` 是 gh 的薄封装，提供四个能力：`EnsureInstalled`（PATH 检测）、`InstallHint`（按平台返回安装命令）、`AuthStatus`（已登录与否）、`Repos`（仓库列表）。

---

## 启动检查流程

`zzauto serve` 启动顺序（详见 [cli.md](./cli.md)）：

1. 加载 `zzauto.yaml` + `ZZAUTO_*` env；
2. aiclibridge 健康检查（不可达则自动安装，`--no-auto-install` 时仅提示退出）；
3. **`ghcli.EnsureInstalled()`**：`exec.LookPath("gh")`，未装则打印 `InstallHint()` 平台命令并以退出码 1 退出；
4. **`ghcli.AuthStatus(ctx)`**：执行 `gh auth status`，退出码 0 视为已登录；非 0 视为未登录，打印 `LoginHint()` 并以退出码 1 退出；
5. 后续 serve 正常启动 HTTP API。

任一前置失败均不会启动 HTTP 服务。退出码统一为 1，stderr 输出可读提示。

> `AuthStatus` 的实现：`gh auth status` 已登录返回 0；未登录返回非 0 退出码（不同 gh 版本可能为 1 或 4）。`ghcli` 把非 0 退出码视为「未登录」而非异常，命令本身缺失（`exec.LookPath` 失败）才视为错误。

---

## 平台安装命令

`InstallHint()` 按 `runtime.GOOS` 返回多行字符串，serve 在未装时直接打印到 stderr：

| 平台 | 命令（任选其一） |
| --- | --- |
| macOS | `xcode-select --install`（Xcode Developer Tools 含 gh）<br>`brew install gh`（Homebrew） |
| Linux | `sudo apt install gh`（Debian/Ubuntu）<br>`sudo dnf install gh`（Fedora/RHEL）<br>`sudo pacman -S github-cli`（Arch） |
| Windows | `winget install GitHub.cli`<br>`choco install gh` |

未识别平台回退到 `https://github.com/cli/cli#installation`。

> 安装后重新执行 `zzauto serve`。若 gh 已在 PATH，`EnsureInstalled` 立即返回 nil 进入下一步。

---

## 登录

`gh auth login` 进入交互式登录向导，按提示选择：

1. **What account?** → `GitHub.com`；
2. **Protocol?** → `HTTPS`（推荐，配合 gittor 的 HTTPS remote）；
3. **Authenticate Git?** → `Yes`；
4. **How to auth?** → `Login with a web browser`（浏览器粘贴一次性码）或 `Paste an authentication token`。

登录成功后 `gh auth status` 输出形如：

```
github.com
  ✓ Logged in to github.com as tgcz2011
  ✓ Active account: true
  ✓ Git operations protocol: https
```

zzauto 不读取 gh 的 token，只依赖 `gh auth status` 的退出码与 `gh repo list` 的输出。

---

## API

zzauto HTTP 暴露两条 gh 相关代理路由：

| 方法 | 路由 | 用途 |
| --- | --- | --- |
| GET | `/api/gh/status` | 返回 `{installed, authenticated, hint}`，UI 用于显示状态徽章 |
| GET | `/api/gh/repos` | 代理 `ghcli.Repos`，返回 `[{nameWithOwner,isPrivate,url,description}]`；未登录返回 401 + login 提示 |

### `GET /api/gh/status`

```json
{
  "installed": true,
  "authenticated": true,
  "hint": ""
}
```

未装时 `installed=false`、`hint` 含 `InstallHint()`；未登录时 `authenticated=false`、`hint` 含 `LoginHint()`。UI 在新建项目弹窗打开时先调此路由显示徽章。

### `GET /api/gh/repos`

已登录返回 200 + 仓库数组：

```json
[
  {"nameWithOwner":"tgcz2011/zzauto","isPrivate":false,"url":"https://github.com/tgcz2011/zzauto","description":"AI 自主编程平台"},
  {"nameWithOwner":"tgcz2011/todo","isPrivate":true,"url":"https://github.com/tgcz2011/todo","description":""}
]
```

未登录返回 401：

```json
{"error":"gh CLI 未登录","hint":"GitHub CLI 未登录，请运行：\n  gh auth login\n..."}
```

`ghcli.Repos` 内部执行 `gh repo list --json nameWithOwner,isPrivate,url,description --limit 100`，最多 100 条。`Repos` 在输出含 `not logged` 或 `auth` 字样时返回哨兵 `ErrNotAuthenticated`，由 HTTP 层翻译为 401。

---

## UI 行为

新建项目弹窗（项目页 →「新建项目」按钮）：

1. 打开时调 `GET /api/gh/status`，顶部显示徽章：`gh 已登录` / `gh 未登录` / `gh 未安装`；
2. 若 `installed=false` 或 `authenticated=false`，禁用「创建」按钮并显示提示；
3. 否则调 `GET /api/gh/repos` 填充仓库下拉，用户选 `nameWithOwner` 后自动填入 repo 字段；
4. 分支字段默认 `main`，可手改。

> 项目创建本身不依赖 gh 仓库存在（`projects.Registry.Create` 只写本地 `project.json`），但 Gittor 提交时若 remote 不存在会失败。建议先在 GitHub 建好仓库再 `gh repo list` 选中。

---

## 与 gittor 的关系

| 维度 | ghcli | gittor |
| --- | --- | --- |
| 用途 | 启动检查 + 仓库列表读取 | git 提交、push、checkout |
| 调用时机 | serve 启动 + UI 新建项目 | Gittor agent 执行 |
| 鉴权来源 | gh 自己的 token 存储（`gh auth login` 写入） | `cfg.GitHub.Token`（HTTPS remote 内嵌）或 SSH key |
| 配置 | 无（用户走 `gh auth login`） | `zzauto.yaml` 的 `github` 段 / `POST /api/github` / env |

两者**完全独立**：gh 的登录态仅用于 `gh repo list`，不影响 gittor 的 push 行为。即使用户 `gh auth login` 登录了，gittor push HTTPS remote 仍需 `github.token`（或改用 SSH remote 走 SSH key）。

> 这是 v0.3.0 的有意设计：避免 zzauto 在自身配置里管理第二份 token；gh 的 token 由 gh 自己管理，gittor 的 token 由 zzauto 配置管理。

---

## 退出码参考

| 场景 | 退出码 | stderr 输出 |
| --- | --- | --- |
| gh 未安装 | 1 | `未检测到 gh CLI\n\n请按平台安装：\n<InstallHint()>` |
| gh 已装但未登录 | 1 | `<LoginHint()>` |
| gh 命令本身异常（如损坏） | 1 | `查询 gh auth 状态失败: <err>` |
| 一切就绪 | 0 | （继续启动 serve） |

---

## 故障排查

### `未检测到 gh CLI` 但已安装

- 确认 `which gh` 能找到；若手动放到非标准路径，把该路径加入 `PATH`。
- macOS 用 `xcode-select --install` 装的是 Xcode Developer Tools，gh 在 `/usr/bin/gh`；Homebrew 装在 `/opt/homebrew/bin/gh`（Apple Silicon）或 `/usr/local/bin/gh`（Intel）。
- Windows 用 winget 装后 gh 在 `C:\Program Files\GitHub CLI\gh.exe`，winget 自动加 PATH；若新开 shell 找不到，重启终端或检查 PATH。

### `gh auth status` 退出码非 0 但实际已登录

- gh 旧版本（<2.0）退出码语义不同；升级到 ≥2.0：`brew upgrade gh` / `apt upgrade gh`。
- 多账号场景：`gh auth status` 在某些版本下若当前账号失效会返回非 0，执行 `gh auth login` 重新登录或 `gh auth switch` 切账号。

### `/api/gh/repos` 返回 401

- serve 启动时通过 `AuthStatus`，但运行中 gh 凭据可能过期（token 被吊销）。
- 重新 `gh auth login` 后无需重启 serve，`/api/gh/repos` 下次调用即恢复（每次都新起 `gh` 进程读凭据）。

### `gh repo list` 速度慢

- 仓库多时 gh 会分页拉取；`ghcli.Repos` 限制 `--limit 100`，最多 100 条。
- 网络慢可设 `https_proxy` 环境变量加速。

---

## 相关文件

| 文件 | 职责 |
| --- | --- |
| `internal/ghcli/ghcli.go` | EnsureInstalled / InstallHint / AuthStatus / LoginHint / Repos |
| `cmd/zzauto/main.go` | serve 启动时的 gh 检查与退出逻辑 |
| `internal/ui/handler.go` | `/api/gh/status`、`/api/gh/repos` 路由实现 |
| `internal/projects/registry.go` | 新建项目时 `repo` 字段来源由 UI 从 `/api/gh/repos` 选取 |

---

## 相关文档

- [multi-projects.md](./multi-projects.md) — 多项目支持与新建项目弹窗
- [cli.md](./cli.md) — `serve` 启动顺序与退出码
- [configuration.md](./configuration.md) — `github` 段（gittor 用的 token）配置
- [quickstart.md](./quickstart.md) — gh CLI 安装步骤
- [aiclibridge.md](./aiclibridge.md) — 另一个启动期依赖（aiclibridge）的检查与自动安装
