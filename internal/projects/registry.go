// Package projects 实现多项目注册表，管理 workspace 下全部项目的元数据。
//
// 每个项目对应 <rootDir>/projects/<id>/ 目录，元数据存于 project.json。
// Registry 提供 List / Get / Create / Update / Delete 等基础操作，是
// v0.3.0 多项目支持的核心数据入口。
package projects

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tgcz2011/zzauto/internal/workspace"
)

// ProjectMeta 描述单个项目的元数据。
type ProjectMeta struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Repo         string    `json:"repo"`          // owner/name 形式
	Branch       string    `json:"branch"`        // 默认 "main"
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Status       string    `json:"status"`        // pending / running / done / failed
	CurrentStage string    `json:"current_stage"` // 当前 agent stage
}

// Registry 管理工作区下全部项目的元数据。
type Registry struct {
	rootDir string // workspace 根目录（如 "./workspace"）
}

// New 创建一个 Registry，rootDir 为 workspace 根目录。
func New(rootDir string) *Registry {
	return &Registry{rootDir: rootDir}
}

// projectsRoot 返回 projects 目录路径。
func (r *Registry) projectsRoot() string {
	return filepath.Join(r.rootDir, "projects")
}

// ProjectDir 返回指定项目目录路径，便于其他包定位。
func (r *Registry) ProjectDir(id string) string {
	return filepath.Join(r.projectsRoot(), id)
}

// projectPath 返回指定项目的 project.json 完整路径。
func (r *Registry) projectPath(id string) string {
	return filepath.Join(r.ProjectDir(id), "project.json")
}

// List 扫描 <rootDir>/projects/*/project.json，按 CreatedAt 倒序返回。
func (r *Registry) List() ([]ProjectMeta, error) {
	pattern := filepath.Join(r.projectsRoot(), "*", "project.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("扫描项目列表失败: %w", err)
	}
	metas := make([]ProjectMeta, 0, len(matches))
	for _, m := range matches {
		var meta ProjectMeta
		if err := readJSON(m, &meta); err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", m, err)
		}
		metas = append(metas, meta)
	}
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].CreatedAt.After(metas[j].CreatedAt)
	})
	return metas, nil
}

// Get 读取单个项目的元数据。不存在时返回包装了 fs.ErrNotExist 的错误。
func (r *Registry) Get(id string) (ProjectMeta, error) {
	var meta ProjectMeta
	if err := readJSON(r.projectPath(id), &meta); err != nil {
		return ProjectMeta{}, err
	}
	return meta, nil
}

// Create 创建一个新项目：生成 ID、创建目录与空 input.md、写 project.json。
//
// branch 为空时默认 "main"；status 初始化为 "pending"，current_stage 为空。
func (r *Registry) Create(name, repo, branch string) (ProjectMeta, error) {
	if branch == "" {
		branch = "main"
	}
	id := workspace.GenerateProjectID()
	projectDir := r.ProjectDir(id)

	// 创建项目子目录
	dirs := []string{
		projectDir,
		filepath.Join(projectDir, "agents"),
		filepath.Join(projectDir, "reports"),
		filepath.Join(projectDir, "runs"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return ProjectMeta{}, fmt.Errorf("创建目录 %s 失败: %w", d, err)
		}
	}

	// 写空 input.md
	inputPath := filepath.Join(projectDir, "input.md")
	if err := os.WriteFile(inputPath, []byte(""), 0o644); err != nil {
		return ProjectMeta{}, fmt.Errorf("写入 input.md 失败: %w", err)
	}

	now := time.Now()
	meta := ProjectMeta{
		ID:        id,
		Name:      name,
		Repo:      repo,
		Branch:    branch,
		CreatedAt: now,
		UpdatedAt: now,
		Status:    "pending",
	}

	if err := writeJSON(r.projectPath(id), meta); err != nil {
		return ProjectMeta{}, fmt.Errorf("写入 project.json 失败: %w", err)
	}
	return meta, nil
}

// Update 更新项目元数据，将 UpdatedAt 设为当前时间并写回 project.json。
func (r *Registry) Update(meta ProjectMeta) error {
	meta.UpdatedAt = time.Now()
	if err := writeJSON(r.projectPath(meta.ID), meta); err != nil {
		return fmt.Errorf("更新 project.json 失败: %w", err)
	}
	return nil
}

// Delete 删除整个项目目录。
func (r *Registry) Delete(id string) error {
	dir := r.ProjectDir(id)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("删除项目目录 %s 失败: %w", dir, err)
	}
	return nil
}

// writeJSON 将 v 序列化为 JSON 并写入 path。
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入文件 %s 失败: %w", path, err)
	}
	return nil
}

// readJSON 读取 path 的 JSON 文件并反序列化到 v。
// 文件不存在时返回原始错误（包含 fs.ErrNotExist）。
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("反序列化 %s 失败: %w", path, err)
	}
	return nil
}
