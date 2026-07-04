//go:build !windows

package daemon

import (
	"os"
	"os/exec"
	"syscall"
)

// detach 设置子进程脱离控制终端（Unix setsid）。
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// processAlive 检查进程是否存活。
// Unix: kill(pid, 0) == nil 表示存活。
var processAlive = func(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// signalProcess 向进程发信号。
var signalProcess = func(pid int, sig os.Signal) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(sig)
}

// termSignal 优雅终止信号（SIGTERM）。
var termSignal os.Signal = syscall.SIGTERM

// killSignal 强制终止信号（SIGKILL）。
var killSignal os.Signal = syscall.SIGKILL
