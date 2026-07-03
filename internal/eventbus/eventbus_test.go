package eventbus

import (
	"sync"
	"testing"
	"time"
)

// TestPublishSubscribe 验证基本的发布/订阅投递。
func TestPublishSubscribe(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch := bus.Subscribe()
	bus.Publish(Event{Type: EventAgentStart, Agent: "listener"})

	select {
	case evt := <-ch:
		if evt.Type != EventAgentStart || evt.Agent != "listener" {
			t.Fatalf("收到的事件不符: %+v", evt)
		}
		if evt.Time.IsZero() {
			t.Fatal("Time 应被自动填充")
		}
	case <-time.After(time.Second):
		t.Fatal("未收到事件")
	}
}

// TestMultipleSubscribers 验证多订阅者均能收到事件。
func TestMultipleSubscribers(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch1 := bus.Subscribe()
	ch2 := bus.Subscribe()

	bus.Publish(Event{Type: EventLog, Data: "hi"})

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case evt := <-ch:
			if evt.Data != "hi" {
				t.Fatalf("订阅者 %d 收到错误数据: %v", i, evt.Data)
			}
		case <-time.After(time.Second):
			t.Fatalf("订阅者 %d 未收到事件", i)
		}
	}
}

// TestBufferFullDrop 验证订阅者 channel 满时发布者不阻塞且丢弃事件。
func TestBufferFullDrop(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch := bus.Subscribe()
	// 灌满缓冲（subscriberBuffer 个）。
	for i := 0; i < subscriberBuffer; i++ {
		bus.Publish(Event{Type: EventLog})
	}
	// 再多发若干个：应被丢弃且不阻塞。
	for i := 0; i < 100; i++ {
		bus.Publish(Event{Type: EventLog})
	}

	// 读回 subscriberBuffer 条，不应阻塞、不应 panic。
	got := 0
loop:
	for {
		select {
		case <-ch:
			got++
		default:
			break loop
		}
	}
	if got != subscriberBuffer {
		t.Fatalf("收到 %d 条，期望 %d 条", got, subscriberBuffer)
	}
}

// TestConcurrentPublish 验证并发发布与订阅的线程安全。
//
// 多个发布者 goroutine 同时向总线发布事件，验证 Publish 在并发下不
// 死锁、不 panic；发布完成后抽干 channel 校验确实收到事件。
func TestConcurrentPublish(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch := bus.Subscribe()
	const publishers = 8
	const perPublisher = 200
	var wg sync.WaitGroup
	wg.Add(publishers)
	for p := 0; p < publishers; p++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perPublisher; i++ {
				bus.Publish(Event{Type: EventLog, Data: i})
			}
		}()
	}
	wg.Wait()

	// 抽干所有已缓冲事件（缓冲 256，故多数被丢弃，但应至少收到若干）。
	received := 0
	for {
		select {
		case <-ch:
			received++
		default:
			goto done
		}
	}
done:
	if received == 0 {
		t.Fatal("未收到任何事件")
	}
	if received > subscriberBuffer {
		t.Fatalf("收到 %d 条，超过缓冲上限 %d", received, subscriberBuffer)
	}
}

// TestCloseIdempotent 验证 Close 幂等且后续 Publish 为空操作。
func TestCloseIdempotent(t *testing.T) {
	bus := New()
	ch := bus.Subscribe()

	bus.Close()
	bus.Close() // 第二次不应 panic

	bus.Publish(Event{Type: EventLog})

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("关闭后 channel 应已关闭")
		}
	case <-time.After(time.Second):
		t.Fatal("等待关闭 channel 超时")
	}
}

// TestSubscribeAfterClose 验证关闭后订阅返回已关闭 channel。
func TestSubscribeAfterClose(t *testing.T) {
	bus := New()
	bus.Close()

	ch := bus.Subscribe()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("应返回已关闭的 channel")
		}
	default:
		t.Fatal("应能立即从已关闭 channel 读到零值")
	}
}
