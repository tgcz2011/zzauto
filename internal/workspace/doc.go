// Package workspace 定义 zzauto 的项目工作区与文档协议。
//
// 文档协议：
//   - desire.md：Listener 产出的用户原始欲望
//   - need.md：Asker 澄清后的需求
//   - spec.md：Planner 规划的规格（Requirements 用 [x]/[ ] 标记完成）
//   - deal.md：Designer 设计、Evaluator 评估的契约
//   - task.md：Manager 拆解的任务清单（勾选语法见下方）
//
// task.md 勾选语法：
//
//	- [ ] T1: 任务描述   未完成
//	- [x] T1: 任务描述   已完成
//
// 每项任务拥有唯一 id（如 T1、T2）。
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

// 阶段常量
const (
	StageListener  = "listener"
	StageAsker     = "asker"
	StagePlanner   = "planner"
	StageDesigner  = "designer"
	StageEvaluator = "evaluator"
	StageManager   = "manager"
	StageExecutor  = "executor"
	StageGenerator = "generator"
	StageGittor    = "gittor"
)

// 状态常量
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// 文档名称常量
const (
	DocDesire = "desire.md"
	DocNeed   = "need.md"
	DocSpec   = "spec.md"
	DocDeal   = "deal.md"
	DocTask   = "task.md"
)

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
