# 配置

zzauto 读取工作目录下的 `zzauto.yaml`，再用 `ZZAUTO_*` 环境变量覆盖。`serve` 启动时会先做 aiclibridge 健康检查。

> 安装与上手见 [quickstart.md](./quickstart.md)；CLI 命令见 [cli.md](./cli.md)。

---

## 加载顺序

`internal/config/config.go` 的 `Load()`：

1. 以 `Default()` 为初始值；
2. 读取 `./zzauto.yaml`（不存在则跳过，其他读取错误返回错误），用 `yaml.Unmarshal` 反序列化覆盖；
3. 调用 `applyEnv` 用 `ZZAUTO_*` 环境变量覆盖（非空才覆盖）。

即优先级：**环境变量 > zzauto.yaml > 默认值**。

> 路径固定为 `./zzauto.yaml`（相对当前工作目录），不支持自定义配置文件路径。

---

## 完整配置字段表

| 字段 | 类型 | 默认值 | 环境变量覆盖 | 说明 |
| --- | --- | --- | --- | --- |
| `listen` | string | `127.0.0.1:8788` | `ZZAUTO_LISTEN` | HTTP 服务监听地址（`host:port`） |
| `aicli_addr` | string | `127.0.0.1:8787` | `ZZAUTO_AICLI_ADDR` | aiclibridge 地址（`host:port`，可带 `http://`/`https://` 前缀，内部归一化） |
| `aicli_key` | string | `""` | `ZZAUTO_AICLI_KEY` | aiclibridge api_key，非空时以 `Authorization: Bearer <key>` 鉴权 |
| `workspace_dir` | string | `./workspace` | `ZZAUTO_WORKSPACE_DIR` | 项目工作区根目录，每个项目在其下 `projects/<projectID>/` |
| `github.remote` | string | `""` | `ZZAUTO_GITHUB_REMOTE` | git 远程仓库地址（SSH 或 HTTPS） |
| `github.branch` | string | `""` | `ZZAUTO_GITHUB_BRANCH` | 目标分支名（如 `main`） |
| `github.token` | string | `""` | `ZZAUTO_GITHUB_TOKEN` | GitHub Personal Access Token（HTTPS 远程 push 时用） |

> `github` 为子结构，环境变量用 `ZZAUTO_GITHUB_*` 分别覆盖其字段。

---

## zzauto.yaml 完整示例

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
```

---

## 环境变量 ZZAUTO_* 列表

| 环境变量 | 对应字段 | 备注 |
| --- | --- | --- |
| `ZZAUTO_LISTEN` | `listen` | 非空覆盖 |
| `ZZAUTO_AICLI_ADDR` | `aicli_addr` | 非空覆盖 |
| `ZZAUTO_AICLI_KEY` | `aicli_key` | 非空覆盖 |
| `ZZAUTO_WORKSPACE_DIR` | `workspace_dir` | 非空覆盖 |
| `ZZAUTO_GITHUB_REMOTE` | `github.remote` | 非空覆盖 |
| `ZZAUTO_GITHUB_BRANCH` | `github.branch` | 非空覆盖 |
| `ZZAUTO_GITHUB_TOKEN` | `github.token` | 非空覆盖 |

升级/安装相关环境变量（非配置文件字段，由安装脚本与 installer 读取）：

| 环境变量 | 作用 |
| --- | --- |
| `GITHUB_MIRROR` | GitHub 下载镜像前缀（中国大陆加速，如 `https://ghproxy.com/`），用于安装脚本与 `upgrade` |
| `https_proxy` | HTTPS 代理地址，安装脚本透传给 `curl` |

---

## GitHub 配置说明

### remote url 格式

支持两种形式：

- **SSH**：`git@github.com:owner/repo.git`（依赖本机已配置 SSH key，`token` 可留空）。
- **HTTPS**：`https://github.com/owner/repo.git`（push 时需 `token`）。

> gittor 的 `EnsureRepo` 会 `git remote add origin <remote>`（origin 未设置时）；`checkout` 到 `branch`（不存在则创建）。

### token 安全处理

`internal/gittor/gittor.go` 对 token 做了多层保护：

1. **不落盘**：使用 HTTPS remote 且配置了 token 时，push 直接推到内嵌 token 的临时 URL：
   ```
   https://x-access-token:<token>@github.com/owner/repo.git
   ```
   不写入 git config，避免 token 落盘泄露。原 URL 已含凭据时先剥离再拼接。
2. **输出脱敏**：`runGit` 的所有 stdout/stderr 输出经 `redact` 处理，把 token 替换为 `***`，避免泄露到日志与事件总线。
3. **UI 脱敏**：`GET /api/config` 返回配置时，`token` 字段返回 `***`（已配置）或空串（未配置），不回显明文。
4. **内存更新不落盘**：`POST /api/github` 仅更新内存中的 `cfg`，不写回 `zzauto.yaml`（UI 包不引入 yaml 依赖）；如需持久化，请手动写入 `zzauto.yaml`。

### 配置方式

- **zzauto.yaml**：启动前写入，适合固定配置（见上文示例）。
- **UI**：启动后在 UI 的 GitHub 配置区填写 remote/branch/token，即时生效（仅内存）。
- **环境变量**：`ZZAUTO_GITHUB_REMOTE` / `ZZAUTO_GITHUB_BRANCH` / `ZZAUTO_GITHUB_TOKEN`，适合 CI/容器场景。

---

## aiclibridge 配置补充

- `aicli_addr` 可带 `http://`/`https://` 前缀与尾部斜杠，客户端会归一化为 `host:port`，请求时自动补 `http://`。
- `serve` 启动时用 5 秒超时做 `GET /healthz` 健康检查；不可达时默认自动安装（curl|sh / irm|iex + 30s 健康轮询），加 `--no-auto-install` flag 则仅提示安装命令并以退出码 1 退出。
- 客户端 HTTP 超时 5 分钟（AI 推理较慢）。
- aiclibridge 的自动安装、手动安装、同步升级与故障排查详见 [aiclibridge.md](./aiclibridge.md)。

---

## 相关文件

| 文件 | 职责 |
| --- | --- |
| `internal/config/config.go` | 配置加载与默认值 |
| `internal/gittor/gittor.go` | token 安全处理 |
| `internal/ui/handler.go` | `/api/config`、`/api/github`（内存更新） |
| `internal/aicli/client.go` | `aicli_addr`/`aicli_key` 使用 |
| `scripts/install.sh` / `install.ps1` | `GITHUB_MIRROR` 等安装期变量 |
