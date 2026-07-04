// Package daemon 负责 zzauto 自身的后台 daemon 管理。
//
// Start fork 子进程脱离终端运行 `zzauto serve`，PID 文件管理；
// Stop 读 PID 发 SIGTERM；Status 检查进程存活。
package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PIDFileName PID 文件名。
const PIDFileName = "zzauto.pid"

// LogFileName daemon 日志文件名。
const LogFileName = "zzauto.log"

// ZZAutoDir zzauto 用户配置目录名。
const ZZAutoDir = ".zzauto"

// pidFilePath 返回 PID 文件绝对路径 ~/.zzauto/zzauto.pid。
// 测试可通过覆写该变量注入临时路径。
var pidFilePath = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户主目录失败: %w", err)
	}
	return filepath.Join(home, ZZAutoDir, PIDFileName), nil
}

// logFilePath 返回日志文件绝对路径 ~/.zzauto/zzauto.log。
var logFilePath = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户主目录失败: %w", err)
	}
	return filepath.Join(home, ZZAutoDir, LogFileName), nil
}

// ensureDir 确保 ~/.zzauto 目录存在。
func ensureDir() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(home, ZZAutoDir), 0o755)
}

// Start fork zzauto serve 子进程脱离终端，写 PID 文件。
// serveArgs 透传给子进程的 serve 子命令（如 ["--listen","0.0.0.0:8788"]）。
// 若 daemon 已在运行返回错误。
func Start(serveArgs []string) error {
	// 检查是否已在运行
	if running, _, _, err := Status(); err != nil {
		return err
	} else if running {
		return fmt.Errorf("daemon 已在运行，请先 stop 或 restart")
	}

	if err := ensureDir(); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	logPath, err := logFilePath()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		logFile.Close()
		return fmt.Errorf("获取当前二进制路径失败: %w", err)
	}

	args := append([]string{"serve"}, serveArgs...)
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil

	// 平台特定的 detach
	detach(cmd)

	// 重定向 stdout/stderr 到日志文件
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("启动 daemon 失败: %w", err)
	}

	// 立即释放日志文件句柄（子进程已持有）
	logFile.Close()

	// 等待子进程初始化（500ms）
	time.Sleep(500 * time.Millisecond)

	// 检查子进程是否还存活（避免启动后立即崩溃）
	if !processAlive(cmd.Process.Pid) {
		return fmt.Errorf("daemon 启动后立即退出，请查看日志: %s", logPath)
	}

	// 写 PID 文件
	pidPath, err := pidFilePath()
	if err != nil {
		return err
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		return fmt.Errorf("写 PID 文件失败: %w", err)
	}

	fmt.Printf("daemon 已启动 (PID=%d)，日志: %s\n", cmd.Process.Pid, logPath)
	return nil
}

// Stop 读 PID 文件，发 SIGTERM，等 5s，仍存活发 SIGKILL。
func Stop() error {
	pid, err := readPID()
	if err != nil {
		return fmt.Errorf("daemon 未在运行: %w", err)
	}
	if !processAlive(pid) {
		// 进程已退出，清理 PID 文件
		removePIDFile()
		return fmt.Errorf("daemon 未在运行（清理残留 PID 文件）")
	}

	// 发 SIGTERM
	if err := signalProcess(pid, termSignal); err != nil {
		return fmt.Errorf("发送终止信号失败: %w", err)
	}

	// 等 5s
	for i := 0; i < 50; i++ {
		if !processAlive(pid) {
			removePIDFile()
			fmt.Printf("daemon (PID=%d) 已停止\n", pid)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 仍存活，发 SIGKILL
	if err := signalProcess(pid, killSignal); err != nil {
		return fmt.Errorf("发送强制终止信号失败: %w", err)
	}
	time.Sleep(500 * time.Millisecond)
	removePIDFile()
	fmt.Printf("daemon (PID=%d) 已强制停止\n", pid)
	return nil
}

// Restart 先 Stop 再 Start。
// 即使 Stop 报"未在运行"也继续 Start（清理状态）。
func Restart(serveArgs []string) error {
	if err := Stop(); err != nil {
		// "未在运行"不算致命错误
		fmt.Printf("stop 警告: %v\n", err)
	}
	return Start(serveArgs)
}

// Status 返回 daemon 运行状态。
// running: 是否在运行；pid: 进程 ID；listen: 监听地址（best-effort，可空）；err: 错误。
func Status() (running bool, pid int, listen string, err error) {
	pidVal, err := readPID()
	if err != nil {
		// PID 文件不存在 → 未运行
		return false, 0, "", nil
	}
	if !processAlive(pidVal) {
		// 残留 PID 文件，进程已退出
		removePIDFile()
		return false, 0, "", nil
	}
	// 读配置取 listen（best-effort）
	listen = readListenFromConfig()
	return true, pidVal, listen, nil
}

// readPID 从 PID 文件读取 PID。
func readPID() (int, error) {
	pidPath, err := pidFilePath()
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, fmt.Errorf("读 PID 文件失败: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("解析 PID 失败: %w", err)
	}
	return pid, nil
}

// removePIDFile 删除 PID 文件（忽略错误）。
func removePIDFile() {
	if pidPath, err := pidFilePath(); err == nil {
		os.Remove(pidPath)
	}
}

// readListenFromConfig best-effort 读 zzauto.yaml 的 listen 字段。
// 这里返回空字符串，调用方（main.go）已有 cfg，可自行打印。
func readListenFromConfig() string {
	return ""
}
