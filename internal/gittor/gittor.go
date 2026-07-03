// Package gittor 是 zzauto 的 GitHub 隔离层。
//
// Gittor 封装所有 git 操作（init / add / commit / push / status），使用 git CLI
// （os/exec 调用 git 命令）而非 gh api，避免频率限制。其他 agent 不直接碰 git，
// 只通过 Gittor 的接口请求，确保 git 操作上下文隔离。commit message 遵循
// conventional commits。
package gittor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// conventionalCommitTypes 是允许的 conventional commit 类型前缀。
var conventionalCommitTypes = map[string]bool{
	"feat":     true,
	"fix":      true,
	"docs":     true,
	"style":    true,
	"refactor": true,
	"perf":     true,
	"test":     true,
	"build":    true,
	"ci":       true,
	"chore":    true,
	"revert":   true,
}

// Gittor 封装对单个 git 仓库的操作。
//
// 字段说明：
//   - repoDir：git 仓库根目录（workspace 项目目录或用户配置的仓库本地路径）
//   - remote：远程仓库地址（https 或 ssh URL，或本地路径）
//   - branch：目标分支名
//   - token：可选 GitHub token，用于 https 鉴权（仅 push 时拼到 URL，不写入 config）
type Gittor struct {
	repoDir string
	remote  string
	branch  string
	token   string
}

// New 创建一个 Gittor 实例。
func New(repoDir, remote, branch, token string) *Gittor {
	return &Gittor{
		repoDir: repoDir,
		remote:  remote,
		branch:  branch,
		token:   token,
	}
}

// EnsureRepo 确保仓库就绪：
//  1. 若 repoDir 不是 git 仓库（无 .git），执行 git init
//  2. 若 remote 已配置但 origin 未设置，执行 git remote add origin <remote>
//  3. checkout 到 branch（不存在则创建）
func (g *Gittor) EnsureRepo(ctx context.Context) error {
	// 1. 检查是否已是 git 仓库
	gitDir := filepath.Join(g.repoDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("检查 .git 失败: %w", err)
		}
		// 不是 git 仓库，初始化
		if _, err := g.runGit(ctx, "init"); err != nil {
			return err
		}
	}

	// 2. 配置 remote（若未配置）
	if g.remote != "" {
		if _, err := g.runGit(ctx, "remote", "get-url", "origin"); err != nil {
			// origin 不存在，添加
			if _, err := g.runGit(ctx, "remote", "add", "origin", g.remote); err != nil {
				return err
			}
		}
	}

	// 3. checkout 到目标分支（不存在则创建）
	if g.branch != "" {
		if _, err := g.runGit(ctx, "checkout", g.branch); err != nil {
			if _, err := g.runGit(ctx, "checkout", "-b", g.branch); err != nil {
				return err
			}
		}
	}
	return nil
}

// CommitAndPush 是核心方法：暂存指定路径、提交并推送到远程。
//
//   - paths 为空时执行 git add -A，否则执行 git add <paths...>
//   - message 应符合 conventional commits（调用方负责格式，Gittor 会校验前缀）
//   - 若配置了 token 且 remote 是 https，push 时直接推到内嵌 token 的 URL，
//     不写入 git config，避免 token 泄露
func (g *Gittor) CommitAndPush(ctx context.Context, paths []string, message string) error {
	// 校验 commit message 前缀
	if err := validateCommitMessage(message); err != nil {
		return err
	}

	// git add
	if len(paths) == 0 {
		if _, err := g.runGit(ctx, "add", "-A"); err != nil {
			return err
		}
	} else {
		addArgs := append([]string{"add"}, paths...)
		if _, err := g.runGit(ctx, addArgs...); err != nil {
			return err
		}
	}

	// git commit
	if _, err := g.runGit(ctx, "commit", "-m", message); err != nil {
		return err
	}

	// git push
	return g.push(ctx)
}

// push 推送到远程。若配置了 token 且 remote 为 https，直接推到内嵌 token 的 URL；
// 否则推到 origin。直接推 URL 而不写入 config，避免 token 落盘泄露。
func (g *Gittor) push(ctx context.Context) error {
	if g.token != "" && strings.HasPrefix(g.remote, "https://") {
		url := g.withTokenURL()
		if _, err := g.runGit(ctx, "push", url, g.branch); err != nil {
			return err
		}
		return nil
	}
	if _, err := g.runGit(ctx, "push", "origin", g.branch); err != nil {
		return err
	}
	return nil
}

// withTokenURL 把 https remote URL 改写为内嵌 token 的形式：
// https://x-access-token:<token>@github.com/owner/repo.git
// 若原 URL 已含凭据，先剥离再拼接。
func (g *Gittor) withTokenURL() string {
	rest := strings.TrimPrefix(g.remote, "https://")
	if at := strings.Index(rest, "@"); at >= 0 {
		// 剥离已有凭据
		rest = rest[at+1:]
	}
	return "https://x-access-token:" + g.token + "@" + rest
}

// Status 返回 git status --porcelain 的摘要（已去除首尾空白）。
func (g *Gittor) Status(ctx context.Context) (string, error) {
	out, err := g.runGit(ctx, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// runGit 在 repoDir 执行 git 子命令，返回合并的 stdout/stderr。
// 失败时返回的错误包含输出内容；所有输出均会脱敏 token，避免泄露到日志。
func (g *Gittor) runGit(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.repoDir
	out, err := cmd.CombinedOutput()
	raw := string(out)
	if err != nil {
		msg := fmt.Sprintf("git %s 失败: %v\n输出: %s", strings.Join(args, " "), err, raw)
		return g.redact(raw), fmt.Errorf("%s", g.redact(msg))
	}
	return g.redact(raw), nil
}

// redact 将字符串中出现的 token 替换为 ***，避免泄露。
func (g *Gittor) redact(s string) string {
	if g.token == "" {
		return s
	}
	return strings.ReplaceAll(s, g.token, "***")
}

// validateCommitMessage 校验 commit message 首行是否符合 conventional commits。
// 允许形如 "feat: ..."、 "feat(scope): ..."、 "feat!: ..."、 "feat(scope)!: ..."。
func validateCommitMessage(msg string) error {
	first := msg
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		first = msg[:i]
	}
	first = strings.TrimSpace(first)

	// 必须包含 ": " 分隔符
	idx := strings.Index(first, ": ")
	if idx < 0 {
		return fmt.Errorf("commit message 不符合 conventional commits（缺少 \": \" 分隔）: %q", first)
	}
	typePart := first[:idx]
	// 去掉 scope，如 "feat(scope)" -> "feat"
	if lp := strings.IndexByte(typePart, '('); lp >= 0 {
		typePart = typePart[:lp]
	}
	// 去掉结尾的 !（表示 breaking change）
	typePart = strings.TrimSuffix(typePart, "!")

	if !conventionalCommitTypes[typePart] {
		return fmt.Errorf("commit message 类型 %q 不在 conventional commits 允许列表内", typePart)
	}
	return nil
}
