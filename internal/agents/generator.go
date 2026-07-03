package agents

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// Generator 是代码实现阶段的 agent：在隔离目录仅读 Executor 准备的指令文件，
// 调用 AI 生成代码、写入指定输出路径，并回答系统自评问题后交付 report。
//
// Generator 严格遵守上下文隔离——只读 agents/generator/instruction.md，
// 不读 desire/need/spec/deal/task.md，确保它只能基于 Executor 投喂的指令
// 工作，无法触达用户原始需求与上游讨论过程。
//
// MVP 采用单 Generator 模式：一次性实现指令中的所有任务。
type Generator struct{}

// NewGenerator 构造一个 Generator 实例。
func NewGenerator() *Generator { return &Generator{} }

// Name 返回 agent 标识，与 workspace 阶段常量 StageGenerator 对应。
func (g *Generator) Name() string { return workspace.StageGenerator }

// instructionReadName 是 Generator 读取的指令文件相对路径（相对 projectDir）。
const instructionReadName = "agents/generator/instruction.md"

// reportDocName 是 Generator 写入的 report 相对路径（相对 projectDir）。
const reportDocName = "reports/generator.md"

// generatorID 是本 Generator 实例的标识，用于 report 标题（# Generator <id> Report）。
const generatorID = "A"

// generatorSystemPrompt 是 Generator 调用 AI 生成代码时使用的系统提示。
//
// 约束 AI 扮演 Generator，只按指令写代码、不提问，严格按 `` ```lang:path ``
// 围栏格式输出每个文件，代码须可运行、符合 spec 验收点。
const generatorSystemPrompt = "你是一名资深的全栈工程师（Generator）。\n\n" +
	"你的职责：仅阅读系统提供的指令文件，按指令一次性完成所有任务的代码实现。\n" +
	"你不得询问任何问题、不得要求更多信息——严格按现有指令产出全部代码。\n\n" +
	"输出格式（必须严格遵守）：\n" +
	"- 每个文件用一个围栏代码块标注，围栏起始行格式为三反引号后跟「语言:相对路径」，例如：\n" +
	"  ```go:code/main.go\n" +
	"  package main\n" +
	"  ...\n" +
	"  ```\n" +
	"- 路径相对项目根，代码类文件应放在 code/ 目录下。\n" +
	"- 一个文件一个代码块，按需输出多个文件。\n" +
	"- 不要输出代码块之外的任何说明文字。\n\n" +
	"代码质量要求：\n" +
	"1. 代码必须可直接运行、可被编译或解释执行，无语法错误。\n" +
	"2. 严格符合指令中 Spec 要点的验收标准。\n" +
	"3. 必要的依赖、入口、配置均需提供，保证开箱即用。\n" +
	"4. 代码内使用中文注释。"

// selfReviewPrompt 是询问 Generator 自评的系统提示，含 schema.go 的
// SelfReviewQuestion 常量（"你觉得你的项目合格了吗？"）。
const selfReviewPrompt = "你是刚刚完成代码实现的 Generator。系统现在向你提出唯一一个问题：\n\n" +
	SelfReviewQuestion + "\n\n" +
	"请如实回答：是否合格，并给出理由。回答应简洁，1-3 句。"

// CodeFile 表示从 AI 输出中解析出的一个代码文件。
type CodeFile struct {
	Path    string
	Content string
}

// Run 执行 Generator 的一次完整工作流：
//  1. 发布 agent_start
//  2. 仅读指令文件 agents/generator/instruction.md（隔离）；缺失返回错误
//  3. 调用 AI 生成代码
//  4. 解析代码块并写入 code/ 目录（AI 未按格式输出则整段当 code/README.md）
//  5. 调用 AI 自评（SelfReviewQuestion）
//  6. 写 report 到 reports/generator.md（RenderDoc 加 frontmatter）
//  7. 发布 doc_update 与 agent_done；失败发布 agent_failed
func (g *Generator) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, g.Name(), map[string]any{
		DataKeyStage: workspace.StageGenerator,
		DataKeyAgent: g.Name(),
	})

	// 2. 仅读指令文件（隔离：不读 desire/need/spec/deal/task.md）
	instruction, err := ws.ReadDoc(instructionReadName)
	if err != nil {
		var failErr error
		if errors.Is(err, fs.ErrNotExist) {
			failErr = fmt.Errorf("指令文件 %s 不存在", instructionReadName)
		} else {
			failErr = fmt.Errorf("读取指令文件失败: %w", err)
		}
		return g.fail(bus, failErr)
	}

	// 3. 调用 AI 生成代码
	output, err := ai.Ask(ctx, generatorSystemPrompt, instruction)
	if err != nil {
		return g.fail(bus, fmt.Errorf("调用 AI 生成代码失败: %w", err))
	}

	// 4. 解析代码块并写入 code/ 目录
	files := parseCodeBlocks(output)
	writtenPaths, err := g.writeGeneratedFiles(ws, files, output)
	if err != nil {
		return g.fail(bus, err)
	}

	// 5. 调用 AI 自评
	codeSummary := buildCodeSummary(ws, writtenPaths)
	selfReview, err := ai.Ask(ctx, selfReviewPrompt, codeSummary)
	if err != nil {
		return g.fail(bus, fmt.Errorf("调用 AI 自评失败: %w", err))
	}

	// 6. 写 report 到 reports/generator.md
	body := buildReportBody(writtenPaths, selfReview)
	rendered := workspace.RenderDoc(workspace.DocMeta{
		Stage:     workspace.StageGenerator,
		Status:    workspace.StatusDone,
		UpdatedAt: time.Now(),
	}, body)
	if err := os.MkdirAll(ws.ReportsDir(), 0o755); err != nil {
		return g.fail(bus, fmt.Errorf("创建 reports 目录失败: %w", err))
	}
	if err := ws.WriteDoc(reportDocName, rendered); err != nil {
		return g.fail(bus, fmt.Errorf("写入 report 失败: %w", err))
	}

	// 7. 发布 doc_update 与 agent_done
	publishEvent(bus, eventbus.EventDocUpdate, g.Name(), map[string]any{
		DataKeyStage: workspace.StageGenerator,
		DataKeyAgent: g.Name(),
		"doc":        reportDocName,
	})
	publishEvent(bus, eventbus.EventAgentDone, g.Name(), map[string]any{
		DataKeyStage: workspace.StageGenerator,
		DataKeyAgent: g.Name(),
	})
	return nil
}

// writeGeneratedFiles 将解析出的代码文件写入 code/ 目录。
//
// 路径处理：若 AI 给出的路径以 "code/" 开头则去掉前缀（输出根已是 code/），
// 否则原样使用，最终统一拼到 code/ 下。若 AI 未按格式输出（files 为空），
// 把整段输出当作 code/README.md 写入，避免内容丢失。
// 返回已写文件的相对路径清单（形如 "code/main.go"）。
func (g *Generator) writeGeneratedFiles(ws *workspace.Workspace, files []CodeFile, rawOutput string) ([]string, error) {
	var paths []string
	if len(files) == 0 {
		// AI 未按格式输出，整段当 code/README.md
		out := filepath.Join("code", "README.md")
		if err := writeCodeFile(ws, out, rawOutput); err != nil {
			return nil, fmt.Errorf("写入 README 失败: %w", err)
		}
		return []string{out}, nil
	}
	for _, f := range files {
		rel := f.Path
		if strings.HasPrefix(rel, "code/") {
			rel = strings.TrimPrefix(rel, "code/")
		}
		out := filepath.Join("code", rel)
		if err := writeCodeFile(ws, out, f.Content); err != nil {
			return nil, fmt.Errorf("写入代码文件 %s 失败: %w", out, err)
		}
		paths = append(paths, out)
	}
	return paths, nil
}

// fail 发布 agent_failed 事件并返回错误，统一错误出口。
func (g *Generator) fail(bus *eventbus.Bus, err error) error {
	publishEvent(bus, eventbus.EventAgentFailed, g.Name(), map[string]any{
		DataKeyStage:  workspace.StageGenerator,
		DataKeyAgent:  g.Name(),
		DataKeyReason: err.Error(),
	})
	return err
}

// parseCodeBlocks 解析 AI 输出中的代码块，识别 `` ```lang:path `` 形式的
// 围栏起始行，到下一个独占一行的 `` ``` `` 为结束。
// 仅识别起始行含冒号且冒号后非空的路径；无路径信息的围栏被跳过。
// 返回的 CodeFile.Path 保留 AI 给出的原始路径（含可能的 "code/" 前缀），
// 前缀剥离由调用方处理。
func parseCodeBlocks(output string) []CodeFile {
	var files []CodeFile
	lines := strings.Split(output, "\n")
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "```") {
			i++
			continue
		}
		// 围栏起始行：```lang:path
		info := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
		var path string
		if idx := strings.Index(info, ":"); idx >= 0 {
			path = strings.TrimSpace(info[idx+1:])
		}
		if path == "" {
			// 无路径信息，跳过该围栏
			i++
			continue
		}
		// 收集直到结束围栏 ```
		i++
		var content []string
		closed := false
		for i < len(lines) {
			if strings.TrimSpace(lines[i]) == "```" {
				closed = true
				i++
				break
			}
			content = append(content, lines[i])
			i++
		}
		// 未闭合也记录已收集内容（宽容处理）
		_ = closed
		files = append(files, CodeFile{
			Path:    path,
			Content: strings.Join(content, "\n"),
		})
	}
	return files
}

// writeCodeFile 将内容写入 projectDir 下的相对路径 relPath，自动创建中间子目录。
func writeCodeFile(ws *workspace.Workspace, relPath, content string) error {
	full := filepath.Join(ws.Path(), relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}

// buildCodeSummary 构造用于自评的代码摘要：列出已写文件路径清单与各文件
// 前几行内容预览，供 AI 在自评时对照。
func buildCodeSummary(ws *workspace.Workspace, paths []string) string {
	var b strings.Builder
	b.WriteString("已完成的文件清单：\n")
	for _, p := range paths {
		b.WriteString(fmt.Sprintf("- %s\n", p))
		data, err := os.ReadFile(filepath.Join(ws.Path(), p))
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		preview := 5
		if len(lines) < preview {
			preview = len(lines)
		}
		b.WriteString("  摘要：\n")
		for i := 0; i < preview; i++ {
			b.WriteString("  " + lines[i] + "\n")
		}
	}
	return b.String()
}

// buildReportBody 根据 writtenPaths 与自评回答构造 report 正文，
// 遵循 schema.go 的 ReportTemplate 结构。
func buildReportBody(paths []string, selfReview string) string {
	var b strings.Builder
	b.WriteString("# Generator " + generatorID + " Report\n")
	b.WriteString("## 完成内容\n")
	for _, p := range paths {
		b.WriteString(fmt.Sprintf("- %s\n", p))
	}
	b.WriteString("\n## 自评\n")
	b.WriteString(strings.TrimRight(selfReview, "\n"))
	b.WriteString("\n")
	return b.String()
}
