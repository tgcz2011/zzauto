//go:build windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// detach Windows 分支：CREATE_NEW_PROCESS_GROUP + DETACHED_PROCESS。
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008, // DETACHED_PROCESS
	}
}

// processAlive 检查进程是否存活。
// Windows 用 tasklist 查询 PID 是否存在。
var processAlive = func(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), fmt.Sprintf("%d", pid))
}

// signalProcess 向进程发信号。
// Windows 无优雅 SIGTERM，统一用 taskkill /T /F 终止进程树。
var signalProcess = func(pid int, sig os.Signal) error {
	return exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid), "/T", "/F").Run()
}

// termSignal 终止信号。Windows 无 SIGTERM 等价物，用 os.Kill。
var termSignal os.Signal = os.Kill

// killSignal 强制终止信号。
var killSignal os.Signal = os.Kill
