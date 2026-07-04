# 统计面板

zzauto v0.3.0 新增统计面板，从 aiclibridge `/v1/stats/*` 端点拉取 token 用量、USD 估算、并发信息，以 4 张卡片 + 模型分布表呈现。本文说明卡片含义、数据源、刷新机制与故障排查。

> aiclibridge 端点定义见 [aiclibridge.md](./aiclibridge.md)；UI 4 页面总览见 [multi-projects.md](./multi-projects.md)。

---

## 概述

统计面板是 UI 顶部导航的第 3 个页面（项目 / 设置 / 统计 / 任务面板），用于回答：

- aiclibridge 总共处理了多少请求、消耗多少 token、估算多少 USD？
- 当前并发多少？队列多长？最大并发多少？
- 各模型的 token 占比如何？哪个模型最贵？

数据全部来自 aiclibridge，zzauto 仅做代理与渲染，不参与统计计算。aiclibridge 重启后统计清零（除非 aiclibridge 自身持久化，详见其仓库文档）。

---

## 4 张卡片

UI 顶部 4 张卡片，分别对应 4 个 aiclibridge 端点：

| 卡片 | 数据源 | aiclibridge 端点 | zzauto 代理路由 | 字段 |
| --- | --- | --- | --- | --- |
| 总览 | `SummaryResp` | `GET /v1/stats/summary` | `GET /api/stats/summary` | total_requests / total_tokens / total_usd / uptime |
| 用量 | `TotalUsage` | `GET /v1/stats/usage` | `GET /api/stats/usage` | prompt_tokens / completion_tokens / total_tokens / requests / usd |
| 并发 | `ConcurrencyResp` | `GET /v1/stats/concurrency` | `GET /api/stats/concurrency` | active / queued / max |
| 定价 | `PricesResp` | `GET /v1/stats/prices` | （仅客户端，未代理） | 每 1M token 价格 |

> 定价卡片当前 UI 不展示（仅 aicli 客户端可读），保留扩展位。前 3 张卡片由 zzauto 代理 aiclibridge 数据。

### 总览卡片

```
┌────────────────────────────────────────┐
│  总览                                  │
│  ─────────────────────────────────     │
│  总请求数      1,234                    │
│  总 token 数   4.5M                     │
│  估算 USD      $12.34                   │
│  运行时长      2d 3h 15m                │
└────────────────────────────────────────┘
```

`uptime` 由 aiclibridge 返回字符串形式（如 `2d 3h 15m`），zzauto 不解析。

### 用量卡片

```
┌────────────────────────────────────────┐
│  用量                                  │
│  ─────────────────────────────────     │
│  prompt tokens      3.2M               │
│  completion tokens  1.3M               │
│  total tokens       4.5M               │
│  总请求数           1,234               │
│  估算 USD           $12.34              │
└────────────────────────────────────────┘
```

`UsageResp` 含 `models` 数组与 `total` 汇总，卡片只展示 `total`，模型明细在下方表格。

### 并发卡片

```
┌────────────────────────────────────────┐
│  并发                                  │
│  ─────────────────────────────────     │
│  active   2                            │
│  queued   0                            │
│  max      8                            │
└────────────────────────────────────────┘
```

`active` 是当前正在推理的请求数，`queued` 是排队中，`max` 是 aiclibridge 配置的最大并发上限。

---

## 模型分布表

用量卡片下方是模型分布表，列出每个模型的 token 占比：

```
┌────────────────────────────────────────────────────────────────────┐
│  模型分布                                                          │
├──────────────────────────┬──────────┬──────────┬────────┬─────────┤
│  模型                    │ 请求     │ token    │ 占比   │ USD     │
├──────────────────────────┼──────────┼──────────┼────────┼─────────┤
│  claude/anthropic/sonnet │ 1,000    │ 3.8M     │ 84.4%  │ $10.20  │
│  gpt-5                   │ 200      │ 0.5M     │ 11.1%  │ $1.80   │
│  claude/anthropic/opus   │ 34       │ 0.2M     │ 4.4%   │ $0.34   │
└──────────────────────────┴──────────┴──────────┴────────┴─────────┘
```

- 数据来自 `UsageResp.Models` 数组，按 `total_tokens` 倒序排列；
- 「占比」= 该模型 `total_tokens` / `UsageResp.Total.TotalTokens`；
- 行数等于 aiclibridge 返回的模型数（无分页）。

---

## 数据源

zzauto 的 aicli 客户端封装：

```go
func (c *Client) Usage(ctx context.Context) (*UsageResp, error)
func (c *Client) Prices(ctx context.Context) (*PricesResp, error)
func (c *Client) Summary(ctx context.Context) (*SummaryResp, error)
func (c *Client) Concurrency(ctx context.Context) (*ConcurrencyResp, error)
```

均走 `getJSON`（`GET` 请求 + JSON 反序列化 + Bearer 鉴权头）。HTTP 层在 `internal/ui/handler.go` 暴露 3 条代理路由（usage/summary/concurrency），UI 直接 fetch 这些路由，不直接访问 aiclibridge。

> 定价端点 `/v1/stats/prices` zzauto HTTP 未代理（UI 不展示），仅 aicli 客户端可读，供未来扩展。

---

## 自动刷新

UI 统计面板每 **30 秒**自动刷新一次（`setInterval`）。可在面板顶部「自动刷新」开关关闭，关闭后需手动点「刷新」按钮。

刷新流程：

1. UI 并发 fetch `/api/stats/summary` + `/api/stats/usage` + `/api/stats/concurrency`；
2. 任一失败显示该卡片为「加载失败」并保留上次数据；
3. 全部成功后整体重渲染 4 卡片 + 模型分布表。

> 刷新是全量替换，不做增量更新。aiclibridge 端统计是累积值，每次刷新都拿最新累积值。

---

## API

### `GET /api/stats/summary`

代理 `aicli.Summary(ctx)`，返回：

```json
{
  "total_requests": 1234,
  "total_tokens": 4500000,
  "total_usd": 12.34,
  "uptime": "2d 3h 15m"
}
```

### `GET /api/stats/usage`

代理 `aicli.Usage(ctx)`，返回：

```json
{
  "models": [
    {"model":"claude/anthropic/claude-sonnet-4.5","prompt_tokens":3200000,"completion_tokens":1300000,"total_tokens":4500000,"requests":1234,"usd":12.34}
  ],
  "total": {
    "prompt_tokens":3200000,
    "completion_tokens":1300000,
    "total_tokens":4500000,
    "requests":1234,
    "usd":12.34
  }
}
```

### `GET /api/stats/concurrency`

代理 `aicli.Concurrency(ctx)`，返回：

```json
{"active":2,"queued":0,"max":8}
```

任一路由失败返回 502 + aiclibridge 错误信息：

```json
{"error":"请求 aiclibridge /v1/stats/summary 失败: ..."}
```

---

## 故障排查

### 4 张卡片全显示「加载失败」

- 检查 aiclibridge 是否在运行：`curl http://127.0.0.1:8787/healthz`；
- 检查 `zzauto.yaml` 的 `aicli_addr` 是否指向正确地址；
- aiclibridge 版本过低不支持 `/v1/stats/*` 端点（v0.3.0 起才支持），升级 aiclibridge。

### 仅定价卡片为空

- 定价端点 zzauto HTTP 未代理，UI 当前不展示，是预期行为。

### 数据不更新

- 关闭「自动刷新」开关后不会自动刷新，需手动点「刷新」；
- aiclibridge 统计是累积值，长时间无请求时数字不变属正常；
- 浏览器缓存：硬刷新（Cmd+Shift+R / Ctrl+Shift+R）。

### USD 估算为 0

- aiclibridge 未配置模型定价时 `usd` 字段为 0；
- 在 aiclibridge 侧配置 `prices` 段（详见其仓库文档）。

---

## 相关文件

| 文件 | 职责 |
| --- | --- |
| `internal/aicli/stats.go` | Usage/Prices/Summary/Concurrency 客户端方法 |
| `internal/ui/handler.go` | `/api/stats/*` 3 条代理路由 |
| `internal/ui/web/app.js` | 统计面板前端渲染与 30s 自动刷新 |
| `internal/config/config.go` | `aicli_addr`/`aicli_key` 字段（决定代理目标） |

---

## 相关文档

- [aiclibridge.md](./aiclibridge.md) — `/v1/stats/*` 端点定义与客户端方法
- [configuration.md](./configuration.md) — `aicli_addr`/`aicli_key` 配置
- [task-panel.md](./task-panel.md) — 任务面板（另一个 v0.3.0 新页面）
- [multi-projects.md](./multi-projects.md) — UI 4 页面总览
