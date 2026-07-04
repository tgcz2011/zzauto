// ghcli 封装 GitHub CLI（gh）的检测、安装提示、auth 状态查询、仓库列表拉取，
// 供 main.go 启动检查与 UI 调用使用。
package ghcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

// 以下为可注入测试点（包级变量），生产使用默认值，测试可临时替换。

// lookPath 判断二进制是否在 PATH（默认 exec.LookPath）。
var lookPath = exec.LookPath

// ghBinaryName gh 二进制名（测试可改为 mock 脚本名）。
var ghBinaryName = "gh"

// runCommand 执行命令并返回合并输出（stdout+stderr）。测试可替换为 fake。
var runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// ErrNotAuthenticated gh CLI 未登录哨兵错误。
var ErrNotAuthenticated = errors.New("gh CLI 未登录")

// Repo 表示一个 GitHub 仓库的精简信息。
type Repo struct {
	NameWithOwner string `json:"nameWithOwner"`
	IsPrivate     bool   `json:"isPrivate"`
	URL           string `json:"url"`
	Description   string `json:"description"`
}

// EnsureInstalled 确保 gh CLI 已安装（在 PATH 中可找到）。
// 未安装时返回包含平台安装提示的错误。
func EnsureInstalled() error {
	if _, err := lookPath(ghBinaryName); err != nil {
		return fmt.Errorf("未检测到 gh CLI\n\n请按平台安装：\n%s", InstallHint())
	}
	return nil
}

// InstallHint 按 runtime.GOOS 返回对应平台的安装提示多行字符串。
func InstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return `macOS（任选其一）：
  xcode-select --install      # 安装 Xcode Developer Tools（含 gh）
  brew install gh             # 或用 Homebrew
`
	case "linux":
		return `Linux（按发行版）：
  sudo apt install gh              # Debian/Ubuntu
  sudo dnf install gh              # Fedora/RHEL
  sudo pacman -S github-cli        # Arch
`
	case "windows":
		return `Windows（任选其一）：
  winget install GitHub.cli
  choco install gh
`
	default:
		return "请参考 https://github.com/cli/cli#installation"
	}
}

// AuthStatus 查询 gh auth 状态。
// 已登录返回 (true, nil)；未登录返回 (false, nil)；命令本身异常返回 (false, err)。
func AuthStatus(ctx context.Context) (bool, error) {
	out, err := runCommand(ctx, ghBinaryName, "auth", "status")
	if err == nil {
		// 退出码 0 但输出含 "not logged" 也视为未登录
		if bytes.Contains(out, []byte("not logged")) {
			return false, nil
		}
		return true, nil
	}
	// 非 0 退出码：未登录是正常状态，不返回 error
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	// 命令本身不存在等异常
	return false, err
}

// LoginHint 返回登录提示多行字符串。
func LoginHint() string {
	return `GitHub CLI 未登录，请运行：
  gh auth login
按提示选择 GitHub.com → HTTPS → 浏览器登录或粘贴 token。
`
}

// Repos 拉取当前登录用户的仓库列表（最多 100 条）。
// 若未登录返回 ErrNotAuthenticated；命令失败返回含输出的错误。
func Repos(ctx context.Context) ([]Repo, error) {
	out, err := runCommand(ctx, ghBinaryName, "repo", "list",
		"--json", "nameWithOwner,isPrivate,url,description", "--limit", "100")
	if err != nil {
		// 未登录或 auth 相关错误返回哨兵错误
		if bytes.Contains(out, []byte("not logged")) || bytes.Contains(out, []byte("auth")) {
			return nil, ErrNotAuthenticated
		}
		return nil, fmt.Errorf("gh repo list 失败: %w (输出=%s)", err, string(out))
	}
	var repos []Repo
	if err := json.Unmarshal(out, &repos); err != nil {
		return nil, fmt.Errorf("解析 gh repo list 输出失败: %w", err)
	}
	return repos, nil
}
