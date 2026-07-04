// bootstrap.go 负责 aiclibridge 的自动安装与升级引导。
//
// serve 时若 aiclibridge 不可达，调用 EnsureInstalled 自动安装；
// upgrade 时调用 UpgradeAiclibridge 同步升级 aiclibridge。
package aicli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// aiclibridge 安装脚本地址与健康检查参数。
const (
	// AicliInstallScriptURL darwin/linux 安装脚本地址。
	AicliInstallScriptURL = "https://github.com/tgcz2011/aiclibridge/raw/main/scripts/install.sh"
	// AicliInstallScriptURLWindows windows 安装脚本地址。
	AicliInstallScriptURLWindows = "https://github.com/tgcz2011/aiclibridge/raw/main/scripts/install.ps1"
	// healthCheckPollInterval 安装后健康检查轮询间隔。
	healthCheckPollInterval = 2 * time.Second
)

// 以下为可注入测试点（包级变量），生产使用默认值，测试可临时替换。

// healthCheckTimeout 安装后健康检查总超时。
var healthCheckTimeout = 30 * time.Second

// installFunc 安装函数。生产为 nil 时使用 installAiclibridge；测试可替换为 fake。
var installFunc func(context.Context) error

// lookPath 判断二进制是否在 PATH（默认 exec.LookPath）。
var lookPath = exec.LookPath

// aicliBinaryName aiclibridge 二进制名。
var aicliBinaryName = "aiclibridge"

// EnsureInstalled 确保 aiclibridge 已安装且可达。
// 先做健康检查，若可达直接返回 nil；不可达则执行安装，安装后轮询健康检查直至通过或超时。
// 整个过程尊重 ctx（用于取消）。
func EnsureInstalled(ctx context.Context, addr, apiKey string) error {
	// 先探测是否已安装且可达
	if err := New(addr, apiKey).Health(ctx); err == nil {
		return nil
	}

	// 不可达则执行安装；优先使用可注入的 installFunc，否则用默认实现
	install := installFunc
	if install == nil {
		install = installAiclibridge
	}
	if err := install(ctx); err != nil {
		return fmt.Errorf("安装 aiclibridge 失败: %w", err)
	}

	// 安装后轮询健康检查，直至通过、超时或 ctx 取消
	deadline := time.Now().Add(healthCheckTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("aiclibridge 安装后健康检查被取消: %w", err)
		}
		if err := New(addr, apiKey).Health(ctx); err == nil {
			return nil
		}
		// 计算剩余时间，超时则返回
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("aiclibridge 安装后健康检查超时")
		}
		// 等待下次轮询，但不超过剩余时间
		wait := healthCheckPollInterval
		if remaining < wait {
			wait = remaining
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("aiclibridge 安装后健康检查被取消: %w", ctx.Err())
		case <-time.After(wait):
		}
	}
}

// installAiclibridge 调用平台对应的安装脚本安装 aiclibridge。
// darwin/linux 走 sh + curl，windows 走 powershell irm。
// 命令输出实时打印到 os.Stderr 便于用户看到进度。
func installAiclibridge(ctx context.Context) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "powershell", "-Command",
			fmt.Sprintf("irm %s | iex", AicliInstallScriptURLWindows))
	default:
		// darwin/linux 及其他类 Unix 系统
		cmd = exec.CommandContext(ctx, "sh", "-c",
			fmt.Sprintf("curl -fsSL %s | sh", AicliInstallScriptURL))
	}
	// 接管 stdin/stdout/stderr，安装进度打印到 stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("执行安装命令失败: %w", err)
	}
	return nil
}

// UpgradeAiclibridge 调用 aiclibridge upgrade 子命令同步升级。
// 若 aiclibridge 不在 PATH，返回明确错误。
func UpgradeAiclibridge() error {
	if _, err := lookPath(aicliBinaryName); err != nil {
		return fmt.Errorf("aiclibridge 未安装，无法同步升级")
	}
	out, err := exec.Command(aicliBinaryName, "upgrade").CombinedOutput()
	if err != nil {
		return fmt.Errorf("aiclibridge upgrade 失败: %w (输出=%s)", err, string(out))
	}
	return nil
}
