package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearZZAutoEnv 清理所有 ZZAUTO_* 环境变量并在测试结束后恢复，
// 保证 applyEnv / LoadFrom 的行为不被外部环境污染。
func clearZZAutoEnv(t *testing.T) {
	t.Helper()
	for _, env := range os.Environ() {
		k, _, ok := strings.Cut(env, "=")
		if !ok || !strings.HasPrefix(k, "ZZAUTO_") {
			continue
		}
		val, existed := os.LookupEnv(k)
		os.Unsetenv(k)
		if existed {
			t.Cleanup(func() {
				_ = os.Setenv(k, val)
			})
		}
	}
}

// TestDefault_HasRoleModels 验证 Default() 返回的 RoleModels 为非 nil 的空 map。
func TestDefault_HasRoleModels(t *testing.T) {
	cfg := Default()
	if cfg.RoleModels == nil {
		t.Fatal("RoleModels is nil, want non-nil empty map")
	}
	if len(cfg.RoleModels) != 0 {
		t.Fatalf("RoleModels len = %d, want 0", len(cfg.RoleModels))
	}
}

// TestSave_Load_RoundTrip 验证 Save 写出后用 LoadFrom 读回，RoleModels 字段不丢失。
func TestSave_Load_RoundTrip(t *testing.T) {
	clearZZAutoEnv(t)

	cfg := Default()
	cfg.RoleModels = map[string]string{
		"listener": "model-a",
		"asker":    "model-b",
	}

	path := filepath.Join(t.TempDir(), "zzauto.yaml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom 失败: %v", err)
	}

	if len(loaded.RoleModels) != 2 {
		t.Fatalf("RoleModels len = %d, want 2", len(loaded.RoleModels))
	}
	if got := loaded.RoleModels["listener"]; got != "model-a" {
		t.Errorf("RoleModels[listener] = %q, want %q", got, "model-a")
	}
	if got := loaded.RoleModels["asker"]; got != "model-b" {
		t.Errorf("RoleModels[asker] = %q, want %q", got, "model-b")
	}
}

// TestApplyEnv_RoleModels 验证 ZZAUTO_ROLE_MODEL_<STAGE> 环境变量被正确解析到 RoleModels。
func TestApplyEnv_RoleModels(t *testing.T) {
	clearZZAutoEnv(t)
	t.Setenv("ZZAUTO_ROLE_MODEL_LISTENER", "model-a")
	t.Setenv("ZZAUTO_ROLE_MODEL_ASKER", "model-b")

	cfg := Default()
	applyEnv(cfg)

	if got := cfg.RoleModels["listener"]; got != "model-a" {
		t.Errorf("RoleModels[listener] = %q, want %q", got, "model-a")
	}
	if got := cfg.RoleModels["asker"]; got != "model-b" {
		t.Errorf("RoleModels[asker] = %q, want %q", got, "model-b")
	}
}

// TestSave_PreservesAllFields 验证 Save -> LoadFrom 往返后所有字段保持一致。
func TestSave_PreservesAllFields(t *testing.T) {
	clearZZAutoEnv(t)

	cfg := Default()
	cfg.Listen = "0.0.0.0:9999"
	cfg.AicliAddr = "1.2.3.4:8080"
	cfg.AicliKey = "secret-key"
	cfg.WorkspaceDir = "/tmp/ws"
	cfg.Github = GithubConfig{
		Remote: "https://github.com/foo/bar.git",
		Branch: "main",
		Token:  "ghp_token",
	}
	cfg.RoleModels = map[string]string{
		"listener": "model-a",
		"asker":    "model-b",
	}

	path := filepath.Join(t.TempDir(), "zzauto.yaml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom 失败: %v", err)
	}

	if loaded.Listen != cfg.Listen {
		t.Errorf("Listen = %q, want %q", loaded.Listen, cfg.Listen)
	}
	if loaded.AicliAddr != cfg.AicliAddr {
		t.Errorf("AicliAddr = %q, want %q", loaded.AicliAddr, cfg.AicliAddr)
	}
	if loaded.AicliKey != cfg.AicliKey {
		t.Errorf("AicliKey = %q, want %q", loaded.AicliKey, cfg.AicliKey)
	}
	if loaded.WorkspaceDir != cfg.WorkspaceDir {
		t.Errorf("WorkspaceDir = %q, want %q", loaded.WorkspaceDir, cfg.WorkspaceDir)
	}
	if loaded.Github != cfg.Github {
		t.Errorf("Github = %+v, want %+v", loaded.Github, cfg.Github)
	}
	if len(loaded.RoleModels) != len(cfg.RoleModels) {
		t.Fatalf("RoleModels len = %d, want %d", len(loaded.RoleModels), len(cfg.RoleModels))
	}
	for k, want := range cfg.RoleModels {
		if got := loaded.RoleModels[k]; got != want {
			t.Errorf("RoleModels[%q] = %q, want %q", k, got, want)
		}
	}
}
