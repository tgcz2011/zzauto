package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Workspace 表示一个项目的工作区，承载文档协议与目录结构。
//
// 目录结构：
//
//	<WorkspaceDir>/projects/<projectID>/
//	  desire.md
//	  need.md
//	  spec.md
//	  deal.md
//	  task.md
//	  agents/<agentName>/
//	  reports/
type Workspace struct {
	rootDir    string
	projectID  string
	projectDir string
}

// New 根据已有的 rootDir 与 projectID 创建 workspace。
func New(rootDir, projectID string) *Workspace {
	return &Workspace{
		rootDir:    rootDir,
		projectID:  projectID,
		projectDir: filepath.Join(rootDir, "projects", projectID),
	}
}

// NewProject 生成新的 projectID 并创建 workspace。
func NewProject(rootDir string) *Workspace {
	return New(rootDir, GenerateProjectID())
}

// GenerateProjectID 生成短 id：日期时间 + 随机 hex。
func GenerateProjectID() string {
	now := time.Now().Format("20060102-150405")
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		// 极小概率失败，退化为纯时间戳
		return now
	}
	return now + "-" + hex.EncodeToString(b)
}

// RootDir 返回 workspace 根目录。
func (w *Workspace) RootDir() string { return w.rootDir }

// ProjectID 返回项目 ID。
func (w *Workspace) ProjectID() string { return w.projectID }

// Path 返回项目目录。
func (w *Workspace) Path() string { return w.projectDir }

// AgentsDir 返回 agents 目录。
func (w *Workspace) AgentsDir() string { return filepath.Join(w.projectDir, "agents") }

// ReportsDir 返回 reports 目录。
func (w *Workspace) ReportsDir() string { return filepath.Join(w.projectDir, "reports") }

// EnsureDirs 创建项目所需的全部目录。
func (w *Workspace) EnsureDirs() error {
	dirs := []string{
		w.projectDir,
		w.AgentsDir(),
		w.ReportsDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", d, err)
		}
	}
	return nil
}

// DocPath 返回指定文档的路径。
func (w *Workspace) DocPath(name string) string {
	return filepath.Join(w.projectDir, name)
}

// WriteDoc 写入文档内容（若项目目录不存在会自动创建）。
func (w *Workspace) WriteDoc(name, content string) error {
	if err := os.MkdirAll(w.projectDir, 0o755); err != nil {
		return fmt.Errorf("创建项目目录失败: %w", err)
	}
	path := w.DocPath(name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("写入文档 %s 失败: %w", path, err)
	}
	return nil
}

// ReadDoc 读取文档内容。
func (w *Workspace) ReadDoc(name string) (string, error) {
	data, err := os.ReadFile(w.DocPath(name))
	if err != nil {
		return "", fmt.Errorf("读取文档 %s 失败: %w", w.DocPath(name), err)
	}
	return string(data), nil
}
