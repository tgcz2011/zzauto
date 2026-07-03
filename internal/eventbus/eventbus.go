// Package eventbus 提供 zzauto 内部的事件发布/订阅机制。
//
// 事件总线用于编排器与 UI 之间的解耦通信：各 agent 在生命周期
// （启动、完成、失败）或文档更新时发布事件，订阅者（如 UI 广播
// 器、日志收集器）通过 channel 异步接收。
//
// 设计要点：
//   - 多订阅者：每个 Subscribe 返回独立的只读 channel
//   - 不阻塞发布者：订阅者 channel 满时直接丢弃事件（select default）
//   - 缓冲 256：应对短时突发，避免无谓丢弃
package eventbus

import (
	"sync"
	"time"
)

// 事件类型常量。新增类型请在此集中声明，避免散落各处。
const (
	// EventAgentStart agent 开始执行。
	EventAgentStart = "agent_start"
	// EventAgentDone agent 成功完成。
	EventAgentDone = "agent_done"
	// EventAgentFailed agent 执行失败。
	EventAgentFailed = "agent_failed"
	// EventDocUpdate 文档被写入/更新。
	EventDocUpdate = "doc_update"
	// EventAskUser 需要向用户提问（如 Asker 交互）。
	EventAskUser = "ask_user"
	// EventLog 通用日志事件。
	EventLog = "log"
)

// 订阅者 channel 缓冲大小。
const subscriberBuffer = 256

// Event 描述一条总线事件。
//
// Type 为事件类型（见上方常量），Agent 为发布事件的 agent 名称
// （对非 agent 事件可为空），Data 为任意负载，Time 为事件发生时间。
type Event struct {
	Type  string
	Agent string
	Data  any
	Time  time.Time
}

// Bus 基于 channel 的发布/订阅事件总线。
//
// 一个 Bus 实例可被多个订阅者共享，Publish 会向所有订阅者投递事件。
// Bus 不是必须显式关闭的；若调用 Close 则所有订阅者 channel 被关闭，
// 此后 Publish 不再投递。
type Bus struct {
	mu          sync.RWMutex
	subscribers []chan Event
	closed      bool
}

// New 创建一个新的事件总线。
func New() *Bus {
	return &Bus{}
}

// Publish 向所有订阅者发布事件。
//
// 若 Time 为零值，自动填充为当前时间。
// 订阅者 channel 已满时丢弃该事件，不阻塞发布者（select default）。
// Bus 已关闭后该调用为空操作。
func (b *Bus) Publish(evt Event) {
	if evt.Time.IsZero() {
		evt.Time = time.Now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for _, ch := range b.subscribers {
		// 非阻塞投递：满了就丢弃，保证发布者不卡。
		select {
		case ch <- evt:
		default:
		}
	}
}

// Subscribe 订阅事件，返回一个只读 channel（缓冲 256）。
//
// 调用方应持续读取该 channel，否则达到缓冲上限后新事件会被丢弃。
// 返回的 channel 在 Bus 关闭时会被关闭。
func (b *Bus) Subscribe() <-chan Event {
	ch := make(chan Event, subscriberBuffer)

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		// 已关闭则立即关闭新订阅者，避免泄露。
		close(ch)
		return ch
	}
	b.subscribers = append(b.subscribers, ch)
	return ch
}

// Close 关闭总线，关闭所有订阅者 channel。
// 关闭后 Publish 不再投递，Subscribe 返回已关闭的 channel。
// 该方法幂等，可多次调用。
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for _, ch := range b.subscribers {
		close(ch)
	}
	b.subscribers = nil
}
