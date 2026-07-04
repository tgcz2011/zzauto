// bootstrap.go 负责 aiclibridge 的自动安装与升级引导。
//
// serve 时若 aiclibridge 不可达，调用 EnsureInstalled 自动安装；
// upgrade 时调用 UpgradeAiclibridge 同步升级 aiclibridge。
package aicli

import (
	"context"
	"fmt"
	"log"
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

// healthCheckTimeout 启动 daemon 后健康检查总超时。
var healthCheckTimeout = 30 * time.Second

// installFunc 安装函数。生产为 nil 时使用 installAiclibridge；测试可替换为 fake。
var installFunc func(context.Context) error

// lookPath 判断二进制是否在 PATH（默认 exec.LookPath）。
var lookPath = exec.LookPath

// aicliBinaryName aiclibridge 二进制名。
var aicliBinaryName = "aiclibridge"

// startDaemonFunc 启动 daemon 函数。生产为 StartDaemon；测试可替换为 fake。
var startDaemonFunc func(context.Context) error = StartDaemon

// EnsureInstalled 确保 aiclibridge 已安装且可达。
// 先做健康检查，若可达直接返回 nil；不可达则：
//   - 已安装（lookPath 命中）：启动 daemon，轮询健康检查
//   - 未安装：执行安装后启动 daemon，轮询健康检查
//
// 整个过程尊重 ctx（用于取消）。避免在 aiclibridge 已装但未启动时误装。
func EnsureInstalled(ctx context.Context, addr, apiKey string) error {
	// 1. 健康检查
	if err := New(addr, apiKey).Health(ctx); err == nil {
		return nil
	}

	// 2. 检测是否已安装
	installed := true
	if _, err := lookPath(aicliBinaryName); err != nil {
		installed = false
	}

	// 3. 未装则安装
	if !installed {
		log.Printf("aiclibridge 未安装，执行安装...")
		install := installFunc
		if install == nil {
			install = installAiclibridge
		}
		if err := install(ctx); err != nil {
			return fmt.Errorf("安装 aiclibridge 失败: %w", err)
		}
	} else {
		log.Printf("aiclibridge 已安装但不可达，启动 daemon...")
	}

	// 4. 启动 daemon
	if err := startDaemonFunc(ctx); err != nil {
		return fmt.Errorf("启动 aiclibridge daemon 失败: %w", err)
	}

	// 5. 轮询健康检查，直至通过、超时或 ctx 取消
	deadline := time.Now().Add(healthCheckTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("aiclibridge 启动后健康检查被取消: %w", err)
		}
		if err := New(addr, apiKey).Health(ctx); err == nil {
			return nil
		}
		// 计算剩余时间，超时则返回
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("aiclibridge 启动后健康检查超时")
		}
		// 等待下次轮询，但不超过剩余时间
		wait := healthCheckPollInterval
		if remaining < wait {
			wait = remaining
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("aiclibridge 启动后健康检查被取消: %w", ctx.Err())
		case <-time.After(wait):
		}
	}
}

// StartDaemon 调用 aiclibridge start 子命令启动后台 daemon。
// aiclibridge start 会 fork 脱离终端的子进程并立即返回，daemon 由 PID 文件管理。
func StartDaemon(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, aicliBinaryName, "start")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("aiclibridge start 失败: %w (输出=%s)", err, string(out))
	}
	return nil
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
