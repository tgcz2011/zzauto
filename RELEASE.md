# zzauto 发版流程规范

本文档定义 zzauto 的版本号规则、发版时机、发版步骤、CHANGELOG 要求、回滚与预发布流程。所有维护者在发布新版本前必须完整阅读并按本流程操作。

> 仓库：https://github.com/tgcz2011/zzauto
> Go module：`github.com/tgcz2011/zzauto`

---

## 1. 版本号规则

zzauto 采用 **语义化版本**（Semantic Versioning），格式为：

```
v<major>.<minor>.<patch>
```

带 `v` 前缀，如 `v0.2.0`。

| 位 | 含义       | 触发条件                                                   |
| -- | ---------- | ---------------------------------------------------------- |
| 第一位 | 大功能更新 | 重大架构变更、不向后兼容的接口改动、里程碑式发布          |
| 第二位 | 小功能更新 | 新增功能、向后兼容的能力增强                               |
| 第三位 | bug 修复   | 仅修复 bug，不新增功能、不改变接口                         |

### 后缀

- **预发布**：`-rc.N`（Release Candidate），如 `v0.2.0-rc.1`，在 GitHub Release 上标记为 prerelease。
- 不使用 `-alpha` / `-beta`，统一用 `-rc.N`。

---

## 2. 何时发版

**每次代码有变化（合并到 main）都应发布对应 release。**

- 修复 bug → patch 版本（如 `v0.1.0` → `v0.1.1`）
- 新增功能 → minor 版本（如 `v0.1.0` → `v0.2.0`）
- 不兼容变更 → major 版本（如 `v0.1.0` → `v1.0.0`）

不发版不影响主分支可用性，但 install.sh 与 upgrade 子命令依赖 `releases/latest/download` 直链，因此即使小修复也建议尽快发版，让用户能通过 `zzauto upgrade` 拉到。

---

## 3. 发版步骤

按以下顺序执行，任何一步失败都应修正后重试，不要跳步。

### 3.1 确保测试通过

```sh
go test ./...
```

如有失败的测试用例，**禁止发版**，先修复。

### 3.2 更新 CHANGELOG.md

- 在文件顶部 `[Unreleased]` 段下方新增本次版本条目（参考第 4 节格式）。
- 将本次新增的变更从 `[Unreleased]` 移入新版本段。
- 日期使用 ISO 8601 格式 `YYYY-MM-DD`，按发版当日填写。

### 3.3 更新版本常量

编辑 `cmd/zzauto/main.go`，修改 `Version` 常量：

```go
const Version = "v0.x.x"
```

注意：GitHub Actions 在构建时会通过 `-ldflags "-X main.Version=${GITHUB_REF_NAME}"` 覆盖该值，但源码中的常量需与即将发布的 tag 保持一致，避免 `go install` / `go run` 场景下版本号不匹配。

### 3.4 提交变更

```sh
git add CHANGELOG.md cmd/zzauto/main.go
git commit -m "chore: release v0.x.x"
```

### 3.5 打 tag 并推送

```sh
git tag v0.x.x
git push origin v0.x.x
```

> 也建议同步推送 main 分支：`git push origin main`。

### 3.6 自动构建与发布

tag `v*` 推送会触发 GitHub Actions（`.github/workflows/release.yml`）自动：

1. 在 5 个平台（darwin-amd64/arm64、linux-amd64/arm64、windows-amd64）构建二进制。
2. 打包压缩文件（macOS/Linux 用 tar.gz，Windows 用 zip）。
3. 生成 sha256 校验文件。
4. 创建 GitHub Release 并上传全部产物。

整个流程通常在 3–5 分钟内完成，可在 Actions 页面查看进度。

### 3.7 验证 Release

打开 https://github.com/tgcz2011/zzauto/releases/tag/v0.x.x，确认 Release 资产包含 **5 个二进制产物 + 5 个 sha256 校验文件**：

- `zzauto-darwin-amd64.tar.gz` + `.sha256`
- `zzauto-darwin-arm64.tar.gz` + `.sha256`
- `zzauto-linux-amd64.tar.gz` + `.sha256`
- `zzauto-linux-arm64.tar.gz` + `.sha256`
- `zzauto-windows-amd64.zip` + `.sha256`

文件名约定与 `scripts/install.sh` 严格一致，否则一键安装脚本与 `zzauto upgrade` 子命令会失败。

最后用一台干净环境验证：

```sh
curl -fsSL https://github.com/tgcz2011/zzauto/raw/main/scripts/install.sh | sh -s -- --version v0.x.x
zzauto version  # 应输出 v0.x.x
```

---

## 4. CHANGELOG 要求

每个版本条目按以下结构组织，段标题用英文以符合 Keep a Changelog 惯例，内容用中文：

```markdown
## [v0.x.x] - 2026-07-XX
### Added
- 新增的功能（向后兼容）

### Changed
- 已有功能的调整

### Fixed
- 修复的 bug

### Removed
- 移除的功能（通常伴随 major 版本升级）
```

如果某段没有内容，可省略该段标题（不要写"无"）。`### Removed` 仅在不兼容删除时才出现，普通版本通常没有。

---

## 5. 回滚

当某个版本存在严重问题需要撤回时：

```sh
# 1. 删除 GitHub Release（保留 tag 便于追溯）
gh release delete v0.x.x --yes

# 2. 删除 tag（本地与远端）
git tag -d v0.x.x
git push origin :refs/tags/v0.x.x
```

也可在 https://github.com/tgcz2011/zzauto/releases 网页操作删除 Release 与 tag。

回滚后：

- `releases/latest` 会自动指向上一个稳定版本。
- 如已发布修复版本（如 `v0.x.1`），无需删除原 tag，直接发新版本覆盖 latest 即可。
- 如回滚后需要重新发同一版本号，**禁止复用 tag**（GitHub 缓存可能导致资产错乱），应递增 patch 后重新发版。

---

## 6. 预发布（Release Candidate）

适用于大版本发布前的最终验证，流程与正式发版一致，仅 tag 与 Release 标记不同：

```sh
git tag v0.2.0-rc.1
git push origin v0.2.0-rc.1
```

GitHub Actions 会识别 `-rc.N` 后缀并自动将 Release 标记为 prerelease，**不会成为 `releases/latest`**，因此不影响 `zzauto upgrade` 拉到的版本。

RC 验证通过后：

1. 在 CHANGELOG 中将 RC 段合并进正式版本段。
2. 按正式发版流程发布 `v0.2.0`。

---

## 7. 相关文件

| 文件                         | 作用                                                 |
| ---------------------------- | ---------------------------------------------------- |
| `RELEASE.md`（本文档）       | 发版流程规范                                          |
| `CHANGELOG.md`               | 变更日志，每次发版必须更新                            |
| `cmd/zzauto/main.go`         | `Version` 常量，每次发版必须更新                      |
| `.github/workflows/release.yml` | tag 触发的自动构建与发布工作流                     |
| `scripts/install.sh`         | 一键安装脚本，依赖 release 资产命名约定              |
| `internal/installer/installer.go` | `upgrade` 子命令实现，依赖 release 直链         |
