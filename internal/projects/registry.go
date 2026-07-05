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
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Repo         string            `json:"repo"`          // owner/name 形式
	Branch       string            `json:"branch"`        // 默认 "main"
	LocalDir     string            `json:"local_dir,omitempty"`  // 本地目录路径（非空则 workspace 指向此目录）
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	Status       string            `json:"status"`        // pending / running / done / failed / paused
	CurrentStage string            `json:"current_stage"` // 当前 agent stage
	PausedStage  string            `json:"paused_stage,omitempty"`  // 暂停时所在阶段
	RoleModels   map[string]string `json:"role_models,omitempty"`   // 项目级模型覆盖
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
// 若项目设置了 LocalDir，则返回 LocalDir（workspace 即本地目录）。
func (r *Registry) ProjectDir(id string) string {
	// 先尝试读 project.json 获取 LocalDir
	var meta ProjectMeta
	if err := readJSON(r.projectPath(id), &meta); err == nil && meta.LocalDir != "" {
		return meta.LocalDir
	}
	return filepath.Join(r.projectsRoot(), id)
}

// projectPath 返回指定项目的 project.json 完整路径（始终在 registry 管理目录下）。
func (r *Registry) projectPath(id string) string {
	return filepath.Join(r.projectsRoot(), id, "project.json")
}

// registryDir 返回指定项目在 registry 管理目录下的路径（存放 project.json）。
func (r *Registry) registryDir(id string) string {
	return filepath.Join(r.projectsRoot(), id)
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
// branch 为空时默认 "main"；localDir 非空时 workspace 指向该目录（不创建 projects/<id>/ 子目录）。
// status 初始化为 "pending"，current_stage 为空。
func (r *Registry) Create(name, repo, branch, localDir string) (ProjectMeta, error) {
	if branch == "" {
		branch = "main"
	}
	id := workspace.GenerateProjectID()

	// registry 管理目录（存放 project.json）
	regDir := r.registryDir(id)
	if err := os.MkdirAll(regDir, 0o755); err != nil {
		return ProjectMeta{}, fmt.Errorf("创建 registry 目录 %s 失败: %w", regDir, err)
	}

	// workspace 目录（文档/代码/runs）
	projectDir := localDir
	if projectDir == "" {
		projectDir = filepath.Join(r.projectsRoot(), id)
	}

	// 创建 workspace 子目录
	dirs := []string{
		projectDir,
		filepath.Join(projectDir, "agents"),
		filepath.Join(projectDir, "reports"),
		filepath.Join(projectDir, "runs"),
		filepath.Join(projectDir, "code"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return ProjectMeta{}, fmt.Errorf("创建目录 %s 失败: %w", d, err)
		}
	}

	// 写空 input.md（仅当不存在时）
	inputPath := filepath.Join(projectDir, "input.md")
	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		if err := os.WriteFile(inputPath, []byte(""), 0o644); err != nil {
			return ProjectMeta{}, fmt.Errorf("写入 input.md 失败: %w", err)
		}
	}

	now := time.Now()
	meta := ProjectMeta{
		ID:        id,
		Name:      name,
		Repo:      repo,
		Branch:    branch,
		LocalDir:  localDir,
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

// Delete 删除项目。若项目使用 LocalDir，仅删除 registry 管理目录（project.json），
// 保留用户的本地目录与代码。
func (r *Registry) Delete(id string) error {
	// 先检查是否有 LocalDir
	var meta ProjectMeta
	hasLocalDir := false
	if err := readJSON(r.projectPath(id), &meta); err == nil && meta.LocalDir != "" {
		hasLocalDir = true
	}

	// 删除 registry 管理目录（含 project.json）
	regDir := r.registryDir(id)
	if err := os.RemoveAll(regDir); err != nil {
		return fmt.Errorf("删除 registry 目录 %s 失败: %w", regDir, err)
	}

	// 若无 LocalDir，也删除 workspace 目录
	if !hasLocalDir {
		wsDir := filepath.Join(r.projectsRoot(), id)
		if err := os.RemoveAll(wsDir); err != nil {
			return fmt.Errorf("删除 workspace 目录 %s 失败: %w", wsDir, err)
		}
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
