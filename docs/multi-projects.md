# 多项目管理

v0.3.0 起 zzauto 把单个 serve 实例从「单一项目」升级为「多项目 Registry」：UI 项目列表 + 新建项目 + 切换 + 删除，每个项目独立 workspace 与 `project.json` 元数据，编排器按需装配。

> 启动顺序与 gh CLI 依赖见 [gh-integration.md](./gh-integration.md)；按需启动编排见 [workflow.md](./workflow.md) 的「按需启动编排」一节。

---

## 概述

v0.2.0 之前的 serve 启动时立即创建一个 workspace 并装配全局 orchestrator，难以同时管理多个需求。v0.3.0 引入 `internal/projects.Registry`：

- 持工作区根目录（`cfg.WorkspaceDir`，默认 `./workspace`）。
- 每个项目对应 `<rootDir>/projects/<id>/` 子目录，元数据存于 `project.json`。
- UI 选中项目后，v0.2.0 路由（`/api/state`、`/api/docs/{name}`、`/api/input`、`/api/asks`、`/api/ask/{id}`）按当前选中项目工作。
- 编排器不再启动时全局装配，改为 UI 点「启动编排」时 `POST /api/projects/{id}/start` 按需装配。

---

## ProjectMeta 字段

`internal/projects/registry.go` 的 `ProjectMeta`：

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `ID` | string | `20060102-150405-<6位hex>`，由 `workspace.GenerateProjectID()` 生成 |
| `Name` | string | UI 创建项目时填写的项目名 |
| `Repo` | string | `owner/name` 形式（来自 gh repo list） |
| `Branch` | string | 默认 `main` |
| `CreatedAt` | time.Time | 创建时间 |
| `UpdatedAt` | time.Time | 最近更新时间（Update 时自动刷新） |
| `Status` | string | `pending` / `running` / `done` / `failed` |
| `CurrentStage` | string | 当前 agent stage（如 `listener` / `asker` / ...） |

`project.json` 示例：

```json
{
  "id": "20260704-120105-a1b2c3",
  "name": "todo-app",
  "repo": "tgcz2011/todo-app",
  "branch": "main",
  "created_at": "2026-07-04T12:01:05+08:00",
  "updated_at": "2026-07-04T12:01:05+08:00",
  "status": "pending",
  "current_stage": ""
}
```

---

## Registry 方法

| 方法 | 行为 |
| --- | --- |
| `New(rootDir)` | 创建 Registry，持工作区根目录 |
| `List()` | 扫描 `projects/*/project.json`，按 `CreatedAt` 倒序返回 `[]ProjectMeta` |
| `Get(id)` | 读取单个项目元数据；不存在返回含 `fs.ErrNotExist` 的错误 |
| `Create(name, repo, branch)` | 生成 ID、建项目目录与 `agents/reports/runs/` 子目录、写空 `input.md`、写 `project.json`；branch 为空时默认 `main` |
| `Update(meta)` | 更新元数据并写回 `project.json`，自动刷新 `UpdatedAt` |
| `Delete(id)` | 删除整个项目目录（含 runs/ 与全部文档） |
| `ProjectDir(id)` | 返回项目目录路径，供 UI handler 定位 runs 目录 |

`Create` 时建的子目录：

```
<rootDir>/projects/<id>/
  project.json
  input.md           # 空
  agents/
  reports/
  runs/              # v0.3.0 新增，存放每个 agent 的 run 事件流
```

---

## 项目目录结构

```
<WorkspaceDir>/projects/<projectID>/
  project.json              # 元数据
  input.md                  # 用户提交的原始需求
  desire.md / need.md / spec.md / deal.md / deal_review.md / task.md
  agents/generator/instruction.md
  code/                     # Generator 产出
  reports/
    generator.md
    evaluated/generator.md
  runs/                     # v0.3.0：每个 agent 的 run 事件流
    listener/<runID>.json
    asker/<runID>.json
    planner/<runID>.json
    ...
```

`runs/<agent>/<runID>.json` 由 `agents.RunWithTracking` 写入，内容是该 run 的完整 `RunEvent` 列表，供任务面板展示（详见 [task-panel.md](./task-panel.md)）。

---

## HTTP API

| 方法 路径 | 说明 |
| --- | --- |
| `GET /api/projects` | 返回 `{projects: [...], current: "<id>"}` |
| `POST /api/projects` | body `{name, repo, branch}`，创建项目并自动选中，返回 `{project: <meta>}` |
| `GET /api/projects/{id}` | 返回 `{project: <meta>}`；不存在 404 |
| `DELETE /api/projects/{id}` | 删除项目目录并停止其运行中编排器；若为当前选中则清空选中；返回 `{ok: true}` |
| `POST /api/projects/{id}/input` | body `{request}`，写入该项目的 `input.md`（带 frontmatter） |
| `POST /api/projects/{id}/start` | 按需装配并启动该项目编排器（409 if 已在运行） |
| `POST /api/projects/{id}/select` | 切换当前选中项目，返回 `{ok: true, current: "<id>"}` |
| `GET /api/projects/{id}/runs` | 该项目的 run 摘要列表（v0.3.0，详见任务面板） |
| `GET /api/projects/{id}/runs/{rid}` | 该项目指定 run 的完整事件时间线 |

---

## UI 布局（项目页）

```
┌──────────────────────────────────────────────────────────┐
│ 顶部导航：[项目] [设置] [统计] [任务面板]      zzauto v0.3.0 │
├──────────────────────────────────────────────────────────┤
│ 项目页                                                    │
│ ┌────────────────────────────────────────────────────┐   │
│ │ [+ 新建项目]   搜索框（按名过滤）                    │   │
│ ├────────────────────────────────────────────────────┤   │
│ │ 项目表格                                            │   │
│ │ ┌──────┬──────────┬──────────────┬──────┬────────┐ │   │
│ │ │ 名称  │ 仓库      │ 分支          │ 状态  │ 操作    │ │   │
│ │ ├──────┼──────────┼──────────────┼──────┼────────┤ │   │
│ │ │todo  │tgcz/todo │main           │done  │选中/删除│ │   │
│ │ │app2  │tgcz/app2 │main           │running│选中/删除│ │   │
│ │ └──────┴──────────┴──────────────┴──────┴────────┘ │   │
│ └────────────────────────────────────────────────────┘   │
│                                                          │
│ 当前项目详情（选中后展示）                                 │
│ ┌────────────────────────────────────────────────────┐   │
│ │ 需求输入框 + [提交] + [启动编排]                     │   │
│ │ 9 agent 状态卡片（pending/running/done/failed）     │   │
│ │ 文档切换：desire/need/spec/deal/task                │   │
│ │ Asker 待回答列表                                     │   │
│ │ GitHub 配置（remote/branch/token）                  │   │
│ └────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────┘
```

### 新建项目弹窗

点「+ 新建项目」打开弹窗：

```
┌──────────────────────────────────────┐
│ 新建项目                       [×]   │
├──────────────────────────────────────┤
│ 项目名称 *  [____________________]   │
│ 仓库       [tgcz2011/todo-app ▼]     │ ← 从 GET /api/gh/repos 拉取
│ 分支       [main_________________]   │
│                                      │
│         [取消]  [创建]                │
└──────────────────────────────────────┘
```

仓库下拉数据来自 `GET /api/gh/repos`（`gh repo list --json`，最多 100 条）；若 gh 未登录返回 401 + login 提示。

---

## 示例：用 curl 创建并启动一个项目

```sh
# 1. 列出项目
curl http://127.0.0.1:8788/api/projects

# 2. 创建项目
curl -X POST http://127.0.0.1:8788/api/projects \
  -H 'Content-Type: application/json' \
  -d '{"name":"todo-app","repo":"tgcz2011/todo-app","branch":"main"}'
# 返回 {"project":{"id":"20260704-...","name":"todo-app",...}}

# 3. 提交需求
curl -X POST http://127.0.0.1:8788/api/projects/20260704-.../input \
  -H 'Content-Type: application/json' \
  -d '{"request":"做一个命令行 todo app"}'

# 4. 启动编排
curl -X POST http://127.0.0.1:8788/api/projects/20260704-.../start

# 5. 查看状态
curl http://127.0.0.1:8788/api/state

# 6. 删除项目
curl -X DELETE http://127.0.0.1:8788/api/projects/20260704-...
```

---

## 并行项目

每个项目在 `POST /api/projects/{id}/start` 时按需装配独立的 orchestrator 与 workspace，存在 `Handler.orchs` map（`projectID -> *orchEntry`）。一份 serve 实例可同时运行多个项目的编排器：

- 各 orchestrator 独立 context（删除项目时 `cancel()`）。
- 事件总线共享，但 `agent_run_event` 等事件含 `Agent` 字段，UI 可按项目过滤。
- `DELETE /api/projects/{id}` 会停止该项目运行中编排器并从 map 移除。

> 注意：`/api/state` 当前为全局流程状态（最后一次事件更新），多项目并行时建议通过 `/api/projects/{id}/runs` 按项目查看 run 详情。

---

## 相关文档

- [gh-integration.md](./gh-integration.md)：gh CLI 安装/登录/仓库列表
- [workflow.md](./workflow.md)：按需启动编排
- [task-panel.md](./task-panel.md)：runs 列表与事件时间线
- [settings.md](./settings.md)：每角色模型配置
- [architecture.md](./architecture.md)：项目注册表分层职责
