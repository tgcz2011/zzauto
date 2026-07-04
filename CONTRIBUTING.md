# 贡献指南

首先，**感谢你考虑为 zzauto 贡献代码！** 欢迎社区以任何形式参与本项目——无论是提 issue、修 bug、加功能、补文档，还是改进测试，都是对项目的重要支持。

zzauto 是一个多层 agent 协作的 AI 自主编程平台，所有协作都在 GitHub（https://github.com/tgcz2011/zzauto）上进行。本指南帮助你顺利地提交改动。

---

## 一、代码修改前

- **先开 issue 讨论大改动**：如果你打算新增功能、调整架构或修改既有行为，请先在 GitHub Issues 中开一个 issue，描述背景、目标与方案，避免重复工作或方向偏差。
- **小改动可直接 PR**：修错字、补注释、修明显 bug 等小改动，可以跳过 issue 直接开 PR，但请在 PR 描述中说明动机。
- **认领 issue**：评论 issue 表示你愿意接手，避免多人重复劳动。

---

## 二、开发流程

1. **Fork + Clone**
   - 在 GitHub 上 Fork 本仓库到你的账号。
   - Clone 到本地：
     ```bash
     git clone https://github.com/<你的用户名>/zzauto.git
     cd zzauto
     git remote add upstream https://github.com/tgcz2011/zzauto.git
     ```

2. **创建分支**
   - 从最新的 `main` 创建分支，使用语义化前缀：
     ```bash
     git checkout -b feat/xxx     # 新功能
     git checkout -b fix/xxx      # bug 修复
     git checkout -b docs/xxx     # 文档
     git checkout -b refactor/xxx # 重构
     git checkout -b test/xxx     # 测试
     git checkout -b chore/xxx    # 杂项
     ```

3. **开发 + 测试**
   - 保持每次提交聚焦、可独立说明意图。
   - 本地运行测试与静态检查（详见「测试要求」）：
     ```bash
     go build ./...
     go vet ./...
     go test ./...
     ```

4. **提交（conventional commits）**
   - 提交信息遵循规范（见下一节）。

5. **Push + 开 PR**
   - 推送到你的 Fork 并向 `tgcz2011/zzauto` 的 `main` 分支发起 Pull Request。
   - PR 描述请按 `.github/pull_request_template.md` 模板填写完整。

---

## 三、提交规范（Conventional Commits）

所有提交信息使用以下前缀，便于自动生成 CHANGELOG 与版本管理：

| 前缀      | 含义            |
| --------- | --------------- |
| `feat`    | 新功能          |
| `fix`     | bug 修复        |
| `docs`    | 文档            |
| `refactor`| 重构（不改行为）|
| `test`    | 测试            |
| `chore`   | 杂项/构建/依赖  |

**格式**：`<type>: <简要描述>`

**示例**：
- `feat: 新增 Asker 挑剔模式`
- `fix: 修复 Executor 在空输入时的 panic`
- `docs: 补充 agents 架构说明`
- `refactor: 抽取 planner 公共校验逻辑`
- `test: 为 evaluator 增加边界用例`
- `chore: 升级 go.mod 依赖`

> 提交正文（可选）用于补充背景、动机或副作用说明，与标题以空行分隔。

---

## 四、文档同步要求（重点）

> **核心约定**：**每次代码修改都要同步更新相关文档**。代码与文档不一致是社区协作最大的隐患，请务必重视。

请根据改动范围，同步更新下列文档：

| 改动类型              | 需要更新的文档                              |
| --------------------- | ------------------------------------------- |
| 用户可见的功能变化    | `README.md`                                 |
| agent 行为 / 架构调整 | `docs/agents.md` 或 `docs/architecture.md`  |
| 配置项 / 环境变量     | `docs/configuration.md`                     |
| CLI 子命令 / 参数     | `docs/cli.md`                               |
| **任何修改**          | `CHANGELOG.md` 的 `[Unreleased]` 段         |

**CHANGELOG 写法**：在 `CHANGELOG.md` 顶部 `[Unreleased]` 段下，按变更类型分组追加一行，例如：
```
### Added
- 新增 Asker 挑剔模式（feat: ...）

### Fixed
- 修复 Executor 空输入 panic（fix: ...）

### Changed
- 重构 planner 公共校验逻辑（refactor: ...）
```

**PR 描述检查项**：在 Pull Request 模板的检查清单中，必须勾选：
- [ ] **已更新相关文档**（README/docs）
- [ ] **已更新 CHANGELOG.md**（`[Unreleased]` 段）

> 若本次改动确无文档影响（例如纯重构未改行为），请在 PR 描述中明确说明「无需更新文档」并简述理由，仍需勾选该项以示已确认。

---

## 五、测试要求

- **新功能必须有测试**：新增的 agent、CLI 子命令、配置项等，需配套单元测试或集成测试。
- **修复 bug 应附带回归测试**：先写一个能复现 bug 的用例，再修复。
- 本地提交前，以下命令必须全部通过：
  ```bash
  go build ./...
  go vet ./...
  go test ./...
  ```
- 测试代码风格参考 `internal/agents/*_test.go`，保持命名一致、断言清晰。

---

## 六、Release 流程

- **由维护者执行**，普通贡献者无需关心发版细节。
- 每次合并到 `main` 后，由维护者评估是否发版，并打 `x.x.x` 形式的 Git tag。
- 详细流程见 `RELEASE.md`（如该文件尚不存在，以维护者实际操作为准）。

---

## 七、代码风格

- **gofmt**：所有 Go 代码必须经 `gofmt` 格式化。
- **中文注释**：包注释、导出符号注释、复杂逻辑注释使用中文，与现有代码风格保持一致。
- **最小改动原则**：只改与本次目标直接相关的代码，避免顺手重构、风格化重排或「顺手修」无关问题；这些应拆成独立 PR。
- **导入分组**：标准库、第三方库、本仓库内部包分组排列，参考 `cmd/zzauto/main.go`。

---

再次感谢你的贡献！如有疑问，欢迎在 Issues 中讨论。
