// Package workspace 定义 zzauto 的项目工作区与文档协议。
//
// v0.6.0 架构精简后的文档协议：
//   - input.md：用户原始需求输入
//   - spec.md：Analyst 分析后产出的结构化规格（Requirements 用 [x]/[ ] 标记完成）
//   - deal.md：Architect 设计的完工协议（含批判性分析、验收标准、风险点）
//   - task.md：Planner 拆解的任务清单
//   - requirements_queue.md：异步需求队列（用户随时追加的新需求）
//   - reports/coder.md：Coder 生成代码后的自评报告
//   - reports/reviewer.md：Reviewer 审查报告
//   - reports/progress.md：Mixor 进度报告
//
// task.md 勾选语法：
//
//	- [ ] T1: 任务描述   未完成
//	- [x] T1: 任务描述   已完成
//
// spec.md 打勾约定：
//
//	### [x] Requirement: Foo   表示该需求已完成
//	### [ ] Requirement: Bar   表示该需求未完成
package workspace

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// 阶段常量（v0.6.0 精简为 5 LLM 角色 + Mixor）
const (
	StageAnalyst   = "analyst"
	StageArchitect = "architect"
	StagePlanner   = "planner"
	StageCoder     = "coder"
	StageReviewer  = "reviewer"
	StageMixor     = "mixor"
)

// 状态常量
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusPaused  = "paused"
)

// 文档名称常量
const (
	DocInput      = "input.md"
	DocSpec       = "spec.md"
	DocDeal       = "deal.md"
	DocTask       = "task.md"
	DocReqQueue   = "requirements_queue.md"
	DocCoderReport  = "reports/coder.md"
	DocReviewReport = "reports/reviewer.md"
	DocProgress     = "reports/progress.md"
)

// AllStages 返回全部阶段（按编排顺序）。
func AllStages() []string {
	return []string{StageAnalyst, StageArchitect, StagePlanner, StageCoder, StageReviewer, StageMixor}
}

// DocMeta 文档 frontmatter 元信息。
type DocMeta struct {
	Stage     string    `yaml:"stage"`
	Status    string    `yaml:"status"`
	UpdatedAt time.Time `yaml:"updated_at"`
}

// ParseDoc 解析文档，分离 frontmatter 与正文。
// 若文档不含 frontmatter，返回零值 DocMeta 与原文。
func ParseDoc(raw string) (DocMeta, string) {
	raw = strings.TrimPrefix(raw, "\ufeff")
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return DocMeta{}, raw
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return DocMeta{}, raw
	}
	fm := strings.Join(lines[1:end], "\n")
	body := strings.TrimPrefix(strings.Join(lines[end+1:], "\n"), "\n")
	var meta DocMeta
	_ = yaml.Unmarshal([]byte(fm), &meta)
	return meta, body
}

// RenderDoc 将元信息与正文渲染为带 frontmatter 的文档。
func RenderDoc(meta DocMeta, body string) string {
	fm, err := yaml.Marshal(meta)
	if err != nil {
		return body
	}
	return fmt.Sprintf("---\n%s---\n%s", string(fm), body)
}
