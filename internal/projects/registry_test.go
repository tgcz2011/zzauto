package projects

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreate(t *testing.T) {
	r := New(t.TempDir())
	meta, err := r.Create("my-proj", "owner/repo", "", "")
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	// project.json 存在
	if _, err := os.Stat(r.projectPath(meta.ID)); err != nil {
		t.Errorf("project.json 不存在: %v", err)
	}
	// input.md 为空文件
	inputPath := filepath.Join(r.ProjectDir(meta.ID), "input.md")
	data, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("读取 input.md 失败: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("input.md 期望空文件, 实际 %q", string(data))
	}
	// 子目录存在
	for _, sub := range []string{"agents", "reports", "runs"} {
		p := filepath.Join(r.ProjectDir(meta.ID), sub)
		fi, err := os.Stat(p)
		if err != nil {
			t.Errorf("期望目录存在 %s: %v", p, err)
			continue
		}
		if !fi.IsDir() {
			t.Errorf("期望是目录 %s", p)
		}
	}
	// 字段正确
	if meta.Name != "my-proj" {
		t.Errorf("Name=%q want %q", meta.Name, "my-proj")
	}
	if meta.Repo != "owner/repo" {
		t.Errorf("Repo=%q want %q", meta.Repo, "owner/repo")
	}
	if meta.Branch != "main" {
		t.Errorf("Branch=%q want %q", meta.Branch, "main")
	}
	if meta.Status != "pending" {
		t.Errorf("Status=%q want %q", meta.Status, "pending")
	}
	if meta.CurrentStage != "" {
		t.Errorf("CurrentStage=%q want empty", meta.CurrentStage)
	}
	if meta.ID == "" {
		t.Errorf("ID 不应为空")
	}
	if meta.CreatedAt.IsZero() || meta.UpdatedAt.IsZero() {
		t.Errorf("CreatedAt/UpdatedAt 不应为零值: %v %v", meta.CreatedAt, meta.UpdatedAt)
	}
}

func TestList(t *testing.T) {
	r := New(t.TempDir())
	for i := 0; i < 3; i++ {
		if _, err := r.Create("proj", "o/r", "main", ""); err != nil {
			t.Fatalf("Create #%d 失败: %v", i, err)
		}
		time.Sleep(time.Millisecond) // 确保 CreatedAt 严格递增
	}
	got, err := r.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List 返回 %d 项, want 3", len(got))
	}
	// 按 CreatedAt 倒序
	for i := 1; i < len(got); i++ {
		if got[i].CreatedAt.After(got[i-1].CreatedAt) {
			t.Errorf("List 未按 CreatedAt 倒序: [%d]=%v > [%d]=%v",
				i, got[i].CreatedAt, i-1, got[i-1].CreatedAt)
		}
	}
}

func TestGet(t *testing.T) {
	r := New(t.TempDir())
	meta, err := r.Create("p", "o/r", "dev", "")
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	got, err := r.Get(meta.ID)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.ID != meta.ID || got.Name != meta.Name || got.Repo != meta.Repo ||
		got.Branch != meta.Branch || got.Status != meta.Status {
		t.Errorf("Get 字段不匹配:\n got=%+v\nwant=%+v", got, meta)
	}
	// 不存在的 id 返回 fs.ErrNotExist
	_, err = r.Get("non-existent-id")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 fs.ErrNotExist, got %v", err)
	}
}

func TestUpdate(t *testing.T) {
	r := New(t.TempDir())
	meta, err := r.Create("p", "o/r", "main", "")
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	oldUpdated := meta.UpdatedAt
	time.Sleep(time.Millisecond)

	meta.Status = "running"
	meta.CurrentStage = "listener"
	if err := r.Update(meta); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	got, err := r.Get(meta.ID)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Status != "running" {
		t.Errorf("Status=%q want %q", got.Status, "running")
	}
	if got.CurrentStage != "listener" {
		t.Errorf("CurrentStage=%q want %q", got.CurrentStage, "listener")
	}
	if !got.UpdatedAt.After(oldUpdated) {
		t.Errorf("UpdatedAt 未更新: old=%v new=%v", oldUpdated, got.UpdatedAt)
	}
}

func TestDelete(t *testing.T) {
	r := New(t.TempDir())
	meta, err := r.Create("p", "o/r", "main", "")
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if err := r.Delete(meta.ID); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if _, err := os.Stat(r.ProjectDir(meta.ID)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望目录已删除, err=%v", err)
	}
	got, err := r.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	for _, m := range got {
		if m.ID == meta.ID {
			t.Errorf("List 仍包含已删除的项目 %s", meta.ID)
		}
	}
}

func TestProjectDir(t *testing.T) {
	root := t.TempDir()
	r := New(root)
	got := r.ProjectDir("abc123")
	if !strings.Contains(got, root) {
		t.Errorf("ProjectDir 未包含 rootDir: got=%s", got)
	}
	if !strings.Contains(got, "abc123") {
		t.Errorf("ProjectDir 未包含 id: got=%s", got)
	}
}
