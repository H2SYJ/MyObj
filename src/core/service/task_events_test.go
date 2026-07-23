package service

import (
	"myobj/src/pkg/enum"
	"myobj/src/pkg/models"
	"testing"
	"time"
)

func receiveTaskEvent(t *testing.T, events <-chan TaskEvent, timeout time.Duration) TaskEvent {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("事件订阅意外关闭")
		}
		return event
	case <-time.After(timeout):
		t.Fatal("等待任务事件超时")
	}
	return TaskEvent{}
}

func TestTaskEventHubIsolatesUsersAndCancelsIdempotently(t *testing.T) {
	hub := NewTaskEventHub()
	userAEvents, cancelA := hub.Subscribe("user-a")
	userBEvents, cancelB := hub.Subscribe("user-b")
	defer cancelB()

	hub.Publish(TaskEvent{Kind: TaskEventDownload, Action: "created", ResourceID: "task-a", UserID: "user-a"}, false)
	event := receiveTaskEvent(t, userAEvents, 100*time.Millisecond)
	if event.ResourceID != "task-a" || event.EventID == 0 || event.Version != 1 {
		t.Fatalf("用户A收到的事件不完整: %#v", event)
	}
	select {
	case event := <-userBEvents:
		t.Fatalf("用户B不应收到用户A事件: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}

	cancelA()
	cancelA()
	if _, ok := <-userAEvents; ok {
		t.Fatal("取消订阅后通道应关闭")
	}
}

func TestTaskEventHubCoalescesProgressAndTerminalPreemptsPending(t *testing.T) {
	hub := NewTaskEventHub()
	events, cancel := hub.Subscribe("user")
	defer cancel()

	hub.Publish(TaskEvent{
		Kind: TaskEventDownload, Action: "updated", ResourceID: "task", UserID: "user",
		Payload: map[string]any{"progress": 10},
	}, true)
	hub.Publish(TaskEvent{
		Kind: TaskEventDownload, Action: "updated", ResourceID: "task", UserID: "user",
		Payload: map[string]any{"progress": 20},
	}, true)
	event := receiveTaskEvent(t, events, 1500*time.Millisecond)
	if event.Payload["progress"] != 20 {
		t.Fatalf("进度事件未合并为最新值: %#v", event.Payload)
	}

	hub.Publish(TaskEvent{
		Kind: TaskEventDownload, Action: "updated", ResourceID: "task", UserID: "user",
		Payload: map[string]any{"progress": 30},
	}, true)
	terminal := downloadTaskEvent(&models.DownloadTask{
		ID: "task", UserID: "user", State: enum.DownloadTaskStateFinished.Value(), Progress: 100,
	}, "updated")
	hub.Publish(terminal, false)
	event = receiveTaskEvent(t, events, 100*time.Millisecond)
	if !event.Terminal || event.Payload["progress"] != 100 {
		t.Fatalf("终态事件未立即发送: %#v", event)
	}
	select {
	case event := <-events:
		t.Fatalf("终态后不应再发送被取消的进度事件: %#v", event)
	case <-time.After(1100 * time.Millisecond):
	}
}

func TestTaskEventHubDisconnectsSlowSubscriber(t *testing.T) {
	hub := NewTaskEventHub()
	events, cancel := hub.Subscribe("user")
	defer cancel()
	for index := 0; index < 65; index++ {
		hub.Publish(TaskEvent{
			Kind: TaskEventDownload, Action: "created", ResourceID: string(rune(index + 1)), UserID: "user",
		}, false)
	}

	received := 0
	for range events {
		received++
	}
	if received != 64 {
		t.Fatalf("慢消费者断开前应保留64条有界队列事件，实际%d条", received)
	}
}
