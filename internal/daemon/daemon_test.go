package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// injectPIDFile 将 pidFilePath 指向临时目录，测试结束自动恢复。
func injectPIDFile(t *testing.T, dir string) {
	t.Helper()
	old := pidFilePath
	pidFilePath = func() (string, error) {
		return filepath.Join(dir, "zzauto.pid"), nil
	}
	t.Cleanup(func() { pidFilePath = old })
}

// TestPIDFile_ReadWrite 写 PID 文件，readPID 读取正确。
func TestPIDFile_ReadWrite(t *testing.T) {
	dir := t.TempDir()
	injectPIDFile(t, dir)

	pidPath, err := pidFilePath()
	if err != nil {
		t.Fatal(err)
	}
	want := 12345
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(want)), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readPID()
	if err != nil {
		t.Fatalf("readPID 意外错误: %v", err)
	}
	if got != want {
		t.Errorf("readPID = %d, want %d", got, want)
	}
}

// TestStatus_NotRunning 无 PID 文件，Status 返回 running=false。
func TestStatus_NotRunning(t *testing.T) {
	dir := t.TempDir()
	injectPIDFile(t, dir)

	running, pid, listen, err := Status()
	if err != nil {
		t.Fatalf("Status 意外错误: %v", err)
	}
	if running {
		t.Errorf("running = true, want false")
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0", pid)
	}
	if listen != "" {
		t.Errorf("listen = %q, want empty", listen)
	}
}

// TestStatus_DeadProcess PID 文件指向不存在的 PID，Status 返回 running=false 且清理 PID 文件。
func TestStatus_DeadProcess(t *testing.T) {
	dir := t.TempDir()
	injectPIDFile(t, dir)

	pidPath, err := pidFilePath()
	if err != nil {
		t.Fatal(err)
	}
	// 999999 在 macOS 上超出 max PID，必然不存在。
	deadPID := 999999
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(deadPID)), 0o644); err != nil {
		t.Fatal(err)
	}

	running, _, _, err := Status()
	if err != nil {
		t.Fatalf("Status 意外错误: %v", err)
	}
	if running {
		t.Errorf("running = true, want false (dead pid)")
	}
	// PID 文件应被清理
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Errorf("PID 文件应被清理, stat err=%v", err)
	}
}

// TestStatus_Running PID 文件指向当前进程 PID，Status 返回 running=true。
func TestStatus_Running(t *testing.T) {
	dir := t.TempDir()
	injectPIDFile(t, dir)

	pidPath, err := pidFilePath()
	if err != nil {
		t.Fatal(err)
	}
	cur := os.Getpid()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cur)), 0o644); err != nil {
		t.Fatal(err)
	}

	running, pid, _, err := Status()
	if err != nil {
		t.Fatalf("Status 意外错误: %v", err)
	}
	if !running {
		t.Errorf("running = false, want true")
	}
	if pid != cur {
		t.Errorf("pid = %d, want %d", pid, cur)
	}
}

// TestStop_NotRunning 无 PID 文件，Stop 返回错误含 "未在运行"。
func TestStop_NotRunning(t *testing.T) {
	dir := t.TempDir()
	injectPIDFile(t, dir)

	err := Stop()
	if err == nil {
		t.Fatal("Stop 期望错误, 实际 nil")
	}
	if !strings.Contains(err.Error(), "未在运行") {
		t.Errorf("Stop 错误 = %q, 期望包含 '未在运行'", err.Error())
	}
}
