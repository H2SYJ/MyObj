package service

import (
	"myobj/src/pkg/enum"
	"myobj/src/pkg/models"
	"sync"
	"sync/atomic"
	"time"
)

const (
	TaskEventDownload  = "download.task"
	TaskEventUpload    = "upload.task"
	TaskEventPackage   = "package.task"
	TaskEventSync      = "sync"
	TaskEventHeartbeat = "heartbeat"
)

// TaskEvent 是前端实时任务通道使用的统一事件结构。
// UserID 仅用于服务端路由，不会序列化到响应中。
type TaskEvent struct {
	EventID    uint64         `json:"event_id"`
	Version    int            `json:"version"`
	Kind       string         `json:"-"`
	Action     string         `json:"action"`
	ResourceID string         `json:"resource_id,omitempty"`
	Terminal   bool           `json:"terminal"`
	Payload    map[string]any `json:"payload,omitempty"`
	OccurredAt time.Time      `json:"occurred_at"`
	UserID     string         `json:"-"`
}

func downloadTaskEvent(task *models.DownloadTask, action string) TaskEvent {
	terminal := task.State == enum.DownloadTaskStateFinished.Value() ||
		task.State == enum.DownloadTaskStateFailed.Value() ||
		task.State == enum.DownloadTaskStateCanceled.Value()
	return TaskEvent{
		Version:    1,
		Kind:       TaskEventDownload,
		Action:     action,
		ResourceID: task.ID,
		Terminal:   terminal,
		UserID:     task.UserID,
		Payload: map[string]any{
			"id":              task.ID,
			"type":            task.Type,
			"state":           task.State,
			"progress":        task.Progress,
			"speed":           task.Speed,
			"downloaded_size": task.DownloadedSize,
			"file_size":       task.FileSize,
			"file_name":       task.FileName,
			"error_msg":       task.ErrorMsg,
			"file_id":         task.FileID,
			"update_time":     task.UpdateTime,
		},
	}
}

func uploadTaskEvent(task *models.UploadTask, action string) TaskEvent {
	terminal := task.Status == "completed" || task.Status == "failed" || task.Status == "aborted"
	progress := 0
	if task.TotalChunks > 0 {
		progress = task.UploadedChunks * 90 / task.TotalChunks
	}
	if task.Status == "processing" && progress < 90 {
		progress = 90
	}
	if task.Status == "completed" {
		progress = 100
	}
	return TaskEvent{
		Version:    1,
		Kind:       TaskEventUpload,
		Action:     action,
		ResourceID: task.ID,
		Terminal:   terminal,
		UserID:     task.UserID,
		Payload: map[string]any{
			"id":               task.ID,
			"status":           task.Status,
			"stage":            task.ProcessingStage,
			"progress":         progress,
			"uploaded_chunks":  task.UploadedChunks,
			"total_chunks":     task.TotalChunks,
			"file_name":        task.FileName,
			"file_size":        task.FileSize,
			"error_message":    task.ErrorMessage,
			"result_file_id":   task.ResultFileID,
			"processing_stage": task.ProcessingStage,
			"update_time":      task.UpdateTime,
			"directory_id":     task.DirectoryID,
			"is_enc":           task.IsEnc,
		},
	}
}

type taskEventSubscriber struct {
	id     uint64
	events chan TaskEvent
}

type pendingTaskEvent struct {
	event TaskEvent
	timer *time.Timer
}

// TaskEventHub 是单实例部署使用的进程内事件中心。
// 业务发布永不等待慢客户端；慢客户端会被断开并在重连后通过 sync 对账。
type TaskEventHub struct {
	mu          sync.Mutex
	sequence    atomic.Uint64
	subscriber  atomic.Uint64
	subscribers map[string]map[uint64]*taskEventSubscriber
	pending     map[string]*pendingTaskEvent
}

func NewTaskEventHub() *TaskEventHub {
	return &TaskEventHub{
		subscribers: make(map[string]map[uint64]*taskEventSubscriber),
		pending:     make(map[string]*pendingTaskEvent),
	}
}

// Subscribe 为指定用户创建独立订阅。返回的取消函数可以安全重复调用。
func (h *TaskEventHub) Subscribe(userID string) (<-chan TaskEvent, func()) {
	id := h.subscriber.Add(1)
	subscriber := &taskEventSubscriber{id: id, events: make(chan TaskEvent, 64)}
	h.mu.Lock()
	if h.subscribers[userID] == nil {
		h.subscribers[userID] = make(map[uint64]*taskEventSubscriber)
	}
	h.subscribers[userID][id] = subscriber
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			userSubscribers := h.subscribers[userID]
			if current, ok := userSubscribers[id]; ok && current == subscriber {
				delete(userSubscribers, id)
				close(subscriber.events)
			}
			if len(userSubscribers) == 0 {
				delete(h.subscribers, userID)
			}
		})
	}
	return subscriber.events, cancel
}

// Publish 发布任务事件。普通进度事件按用户、类型和资源合并为每秒最多一次。
func (h *TaskEventHub) Publish(event TaskEvent, coalesce bool) {
	if h == nil || event.UserID == "" || event.Kind == "" {
		return
	}
	if event.Version == 0 {
		event.Version = 1
	}
	key := event.UserID + "\x00" + event.Kind + "\x00" + event.ResourceID
	if event.Terminal || event.Action != "updated" {
		h.mu.Lock()
		if pending, ok := h.pending[key]; ok {
			pending.timer.Stop()
			delete(h.pending, key)
		}
		h.mu.Unlock()
		h.dispatch(event)
		return
	}
	if !coalesce {
		h.dispatch(event)
		return
	}

	h.mu.Lock()
	if pending, ok := h.pending[key]; ok {
		pending.event = event
		h.mu.Unlock()
		return
	}
	pending := &pendingTaskEvent{event: event}
	pending.timer = time.AfterFunc(time.Second, func() {
		h.mu.Lock()
		current, ok := h.pending[key]
		if ok && current == pending {
			delete(h.pending, key)
		}
		h.mu.Unlock()
		if ok && current == pending {
			h.dispatch(current.event)
		}
	})
	h.pending[key] = pending
	h.mu.Unlock()
}

func (h *TaskEventHub) dispatch(event TaskEvent) {
	event.EventID = h.sequence.Add(1)
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for id, subscriber := range h.subscribers[event.UserID] {
		select {
		case subscriber.events <- event:
		default:
			delete(h.subscribers[event.UserID], id)
			close(subscriber.events)
		}
	}
	if len(h.subscribers[event.UserID]) == 0 {
		delete(h.subscribers, event.UserID)
	}
}
