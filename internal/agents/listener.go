// Package agents 中 Listener Agent 的实现。
//
// Listener 负责听取用户通过 UI 提交的原始需求（input.md），调用 AI
// 在其基础上补充改进点（可访问性、错误处理、边界情况、性能、安全、
// 可维护性等维度），产出符合 schema 约定的 desire.md 文档。
package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// listenerSystemPrompt 是 Listener 调用 AI 时使用的系统提示词。
//
// 要求 AI 扮演 Listener 角色：保留用户原始需求原话，并从多个工程维度
// 补充改进点，最终输出严格符合 schema.go 中 DesireTemplate 约定的
// Markdown 正文（# 用户需求 段 + # 改进点 分点列表）。
const listenerSystemPrompt = `你是 zzauto 多层 agent 编程平台中的 Listener Agent。

【职责】
听取用户提交的原始需求，在其基础上补充改进点，产出 desire.md 文档正文。

【输出格式】严格遵循以下 Markdown 结构，不得增减标题、不得使用代码围栏包裹、不得添加任何前后说明文字：

# 用户需求
<完整保留用户原始需求文本，可多行，不要改写、不要概括>

# 改进点
- <改进点 1>
- <改进点 2>
- ...

【改进点维度】请从以下维度审视需求并补充与该需求切实相关的改进点（不要泛泛而谈、不要罗列无关维度）：
- 可访问性：键盘导航、屏幕阅读器支持、色彩对比度、ARIA 属性等
- 错误处理：异常路径、用户友好的错误提示、重试与回退策略
- 边界情况：空输入、超长输入、并发访问、零值、数值溢出
- 性能：懒加载、缓存、防抖/节流、大列表虚拟化、资源按需加载
- 安全：输入校验与转义、XSS/CSRF 防护、鉴权与越权、敏感信息脱敏
- 可维护性：模块拆分、命名清晰、配置化、可测试性、依赖解耦

【约束】
1. 「用户需求」段必须原样保留用户原话，不得删改、重排或概括。
2. 「改进点」每项以「- 」开头，描述需简洁、具体、可执行，避免空泛口号。
3. 直接输出文档正文，不要包裹在 Markdown 代码围栏中。
4. 不要输出 frontmatter（由系统自动添加）。
5. 不要添加额外标题、段落或解释性文字。`

// Listener 听取用户原始需求并产出 desire.md 的 agent。
//
// 它读取工作区中的 input.md（用户通过 UI 提交），调用 AI 丰富需求
// 与改进点，将结果以带 frontmatter 的形式写入 desire.md。
type Listener struct{}

// NewListener 创建一个 Listener 实例。
func NewListener() *Listener {
	return &Listener{}
}

// Name 返回 agent 标识 "listener"。
func (l *Listener) Name() string {
	return "listener"
}

// Run 执行 Listener 的一次完整工作流：
//   - 发布 agent_start 事件
//   - 读取用户原始需求 input.md；若不存在则发布 ask_user 事件并返回错误
//   - 调用 AI 丰富需求，生成符合 desire.md schema 的正文
//   - 渲染带 frontmatter 的文档并写入 desire.md
//   - 发布 doc_update 与 agent_done 事件
//
// 任一步骤失败时发布 agent_failed 事件并返回错误。
func (l *Listener) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus) error {
	bus.Publish(eventbus.Event{
		Type:  eventbus.EventAgentStart,
		Agent: l.Name(),
	})

	// 读取用户通过 UI 提交的原始需求
	userRequest, err := ws.ReadDoc("input.md")
	if err != nil {
		// input.md 不存在：请求用户先通过 UI 提交需求
		bus.Publish(eventbus.Event{
			Type:  eventbus.EventAskUser,
			Agent: l.Name(),
			Data:  map[string]string{"prompt": "请先通过 UI 提交你的需求"},
		})
		return fmt.Errorf("用户需求 input.md 不存在，请通过 UI 提交")
	}

	// 调用 AI 丰富需求与改进点，得到 desire.md 正文
	body, err := ai.Ask(ctx, listenerSystemPrompt, userRequest)
	if err != nil {
		bus.Publish(eventbus.Event{
			Type:  eventbus.EventAgentFailed,
			Agent: l.Name(),
			Data:  map[string]string{"reason": err.Error()},
		})
		return fmt.Errorf("调用 AI 丰富需求失败: %w", err)
	}

	// 渲染带 frontmatter 的文档
	rendered := workspace.RenderDoc(workspace.DocMeta{
		Stage:     workspace.StageListener,
		Status:    workspace.StatusDone,
		UpdatedAt: time.Now(),
	}, body)

	// 写入 desire.md
	if err := ws.WriteDoc(workspace.DocDesire, rendered); err != nil {
		bus.Publish(eventbus.Event{
			Type:  eventbus.EventAgentFailed,
			Agent: l.Name(),
			Data:  map[string]string{"reason": err.Error()},
		})
		return fmt.Errorf("写入 desire.md 失败: %w", err)
	}

	// 通知 desire 文档已更新
	bus.Publish(eventbus.Event{
		Type:  eventbus.EventDocUpdate,
		Agent: l.Name(),
		Data:  map[string]string{"doc": "desire"},
	})

	// 完成
	bus.Publish(eventbus.Event{
		Type:  eventbus.EventAgentDone,
		Agent: l.Name(),
	})
	return nil
}
