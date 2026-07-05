// Package agents 中 Coder Agent 的实现。
//
// Coder（旧 Generator 重命名）：读取指令文件 agents/coder/instruction.md
// （由 orchestrator 的 buildCoderInstruction 工具函数生成），调用 AI 生成代码
// 并写入 code/ 目录，同时写自评报告 reports/coder.md。
//
// 代码块格式（v0.6.0）：```language\npath: filepath\n内容\n```
// 围栏起始行为三反引号 + 语言；块内首行以 "path:" 开头标识文件路径；
// 其余行作为文件内容。
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

// coderInstructionReadName 是 Coder 读取的指令文件相对路径（相对 projectDir）。
const coderInstructionReadName = "agents/coder/instruction.md"

// coderSystemPrompt 是 Coder 调用 AI 生成代码时使用的系统提示词。
const coderSystemPrompt = "你是代码编写者。根据指令文件生成代码。代码用 ```language\npath: filepath\n内容\n``` 围栏格式输出。生成后写自评报告 reports/coder.md，包含完成清单和自评。"

// CodeFile 表示从 AI 输出中解析出的一个代码文件。
type CodeFile struct {
	Path    string
	Content string
}

// Coder 代码编写者：根据指令文件生成代码与自评报告。
type Coder struct{}

// NewCoder 构造一个 Coder 实例。
func NewCoder() *Coder { return &Coder{} }

// Name 返回 agent 标识，与 workspace.StageCoder 对应。
func (c *Coder) Name() string { return workspace.StageCoder }

// Run 执行 Coder 的一次完整工作流：
//  1. 发布 agent_start
//  2. 读指令文件 agents/coder/instruction.md（隔离）；缺失返回错误
//  3. 调用 AI（RunStream 流式）生成代码
//  4. 解析代码块并写入 code/ 目录（无 path 行的围栏跳过；若无任何代码块则整段当 code/README.md）
//  5. 写 reports/coder.md 自评报告
//  6. 发布 doc_update 与 agent_done；失败发布 agent_failed
func (c *Coder) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus, resolver ModelResolver) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, c.Name(), map[string]any{
		DataKeyStage: workspace.StageCoder,
		DataKeyAgent: c.Name(),
	})

	// 2. 仅读指令文件（隔离）
	instruction, err := ws.ReadDoc(coderInstructionReadName)
	if err != nil {
		var failErr error
		if errors.Is(err, fs.ErrNotExist) {
			failErr = fmt.Errorf("指令文件 %s 不存在", coderInstructionReadName)
		} else {
			failErr = fmt.Errorf("读取指令文件失败: %w", err)
		}
		return c.fail(bus, failErr)
	}

	// 3. 调用 AI 生成代码（流式，便于 UI 实时查看）
	model := ResolveModel(resolver, workspace.StageCoder)
	output, _, err := RunWithTracking(ctx, ws, bus, ai, c.Name(), model, coderSystemPrompt, instruction)
	if err != nil {
		return c.fail(bus, fmt.Errorf("调用 AI 生成代码失败: %w", err))
	}

	// 4. 解析代码块并写入 code/ 目录
	files := parseCodeBlocks(output)
	writtenPaths, err := c.writeGeneratedFiles(ws, files, output)
	if err != nil {
		return c.fail(bus, err)
	}

	// 5. 写 reports/coder.md 自评报告
	reportBody := buildCoderReportBody(writtenPaths, output)
	if err := os.MkdirAll(ws.ReportsDir(), 0o755); err != nil {
		return c.fail(bus, fmt.Errorf("创建 reports 目录失败: %w", err))
	}
	rendered := workspace.RenderDoc(workspace.DocMeta{
		Stage:     workspace.StageCoder,
		Status:    workspace.StatusDone,
		UpdatedAt: time.Now(),
	}, reportBody)
	if err := ws.WriteDoc(workspace.DocCoderReport, rendered); err != nil {
		return c.fail(bus, fmt.Errorf("写入 coder report 失败: %w", err))
	}

	// 6. 发布 doc_update 与 agent_done
	publishEvent(bus, eventbus.EventDocUpdate, c.Name(), map[string]any{
		DataKeyStage: workspace.StageCoder,
		DataKeyAgent: c.Name(),
		"doc":        workspace.DocCoderReport,
	})
	publishEvent(bus, eventbus.EventAgentDone, c.Name(), map[string]any{
		DataKeyStage: workspace.StageCoder,
		DataKeyAgent: c.Name(),
	})
	return nil
}

// writeGeneratedFiles 将解析出的代码文件写入 code/ 目录。
//
// 路径处理：若 AI 给出的路径以 "code/" 开头则去掉前缀（输出根已是 code/），
// 否则原样使用，最终统一拼到 code/ 下。若 AI 未按格式输出（files 为空），
// 把整段输出当作 code/README.md 写入，避免内容丢失。
// 返回已写文件的相对路径清单（形如 "code/main.go"）。
func (c *Coder) writeGeneratedFiles(ws *workspace.Workspace, files []CodeFile, rawOutput string) ([]string, error) {
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
func (c *Coder) fail(bus *eventbus.Bus, err error) error {
	publishEvent(bus, eventbus.EventAgentFailed, c.Name(), map[string]any{
		DataKeyStage:  workspace.StageCoder,
		DataKeyAgent:  c.Name(),
		DataKeyReason: err.Error(),
	})
	return err
}

// parseCodeBlocks 解析 AI 输出中的代码块。
//
// v0.6.0 格式：
//
//	```language
//	path: filepath
//	<内容>
//	```
//
// 围栏起始行为三反引号 + 可选语言标识；块内首行若以 "path:" 开头，
// 取其后的路径作为文件路径；其余行作为文件内容。
// 无 path 行的围栏被跳过。返回的 CodeFile.Path 保留 AI 给出的原始路径
// （含可能的 "code/" 前缀），前缀剥离由调用方处理。
// 未闭合的围栏也记录已收集内容（宽容处理）。
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
		// 围栏起始行：```language（v0.6.0 不再在起始行写 path）
		i++
		if i >= len(lines) {
			break
		}
		// 块内首行：path: filepath
		firstLine := strings.TrimSpace(lines[i])
		var path string
		var contentStart int
		if strings.HasPrefix(firstLine, "path:") {
			path = strings.TrimSpace(strings.TrimPrefix(firstLine, "path:"))
			contentStart = i + 1
		} else {
			// 无 path 行，跳过该围栏
			// 推进到下一个 ``` 结束围栏
			for i < len(lines) && strings.TrimSpace(lines[i]) != "```" {
				i++
			}
			if i < len(lines) {
				i++ // 跳过结束围栏
			}
			continue
		}
		// 收集内容直到结束围栏 ```
		i = contentStart
		var content []string
		for i < len(lines) {
			if strings.TrimSpace(lines[i]) == "```" {
				i++
				break
			}
			content = append(content, lines[i])
			i++
		}
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

// buildCoderReportBody 根据已写文件路径与 AI 原始输出构造 coder report 正文。
//
// 正文结构：
//
//	# Coder Report
//	## 完成内容
//	- <path 1>
//	- <path 2>
//	## 自评
//	<AI 输出中除代码块外的文本，或默认提示>
func buildCoderReportBody(paths []string, aiOutput string) string {
	var b strings.Builder
	b.WriteString("# Coder Report\n")
	b.WriteString("## 完成内容\n")
	for _, p := range paths {
		b.WriteString(fmt.Sprintf("- %s\n", p))
	}
	b.WriteString("\n## 自评\n")
	review := extractSelfReview(aiOutput)
	b.WriteString(strings.TrimRight(review, "\n"))
	b.WriteString("\n")
	return b.String()
}

// extractSelfReview 从 AI 输出中提取代码块之外的文本作为自评。
//
// 简单策略：按行扫描，剔除 ``` 围栏内的所有行，保留围栏外的文本。
// 若围栏外无文本，返回默认提示。
func extractSelfReview(output string) string {
	lines := strings.Split(output, "\n")
	var out []string
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		out = append(out, line)
	}
	review := strings.TrimSpace(strings.Join(out, "\n"))
	if review == "" {
		return "（AI 未提供自评文本）"
	}
	return review
}
