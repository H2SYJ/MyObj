package handlers

import (
	"myobj/src/core/service"
	"myobj/src/internal/api/middleware"
	"myobj/src/pkg/cache"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type EventHandler struct {
	events            *service.TaskEventHub
	cache             cache.Cache
	factory           service.ServerFactoryInterface
	heartbeatInterval time.Duration
	maxConnectionAge  time.Duration
}

func NewEventHandler(events *service.TaskEventHub, cacheLocal cache.Cache, factory service.ServerFactoryInterface) *EventHandler {
	return &EventHandler{
		events:            events,
		cache:             cacheLocal,
		factory:           factory,
		heartbeatInterval: 15 * time.Second,
		maxConnectionAge:  5 * time.Minute,
	}
}

func (h *EventHandler) Router(group *gin.RouterGroup) {
	verify := middleware.NewAuthMiddleware(h.cache,
		h.factory.GetRepository().ApiKey(),
		h.factory.GetRepository().User(),
		h.factory.GetRepository().GroupPower(),
		h.factory.GetRepository().Power())
	group.GET("/events", verify.Verify(), h.Stream)
}

func (h *EventHandler) Stream(c *gin.Context) {
	userID := c.GetString("userID")
	events, cancel := h.events.Subscribe(userID)
	defer cancel()

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	_, _ = c.Writer.WriteString("retry: 1000\n\n")
	c.SSEvent(service.TaskEventSync, service.TaskEvent{
		Version:    1,
		Kind:       service.TaskEventSync,
		Action:     "sync",
		OccurredAt: time.Now().UTC(),
	})
	c.Writer.Flush()

	heartbeatInterval := h.heartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = 15 * time.Second
	}
	maxConnectionAge := h.maxConnectionAge
	if maxConnectionAge <= 0 {
		maxConnectionAge = 5 * time.Minute
	}
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	maxAge := time.NewTimer(maxConnectionAge)
	defer maxAge.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-maxAge.C:
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			c.SSEvent(event.Kind, event)
			c.Writer.Flush()
		case now := <-heartbeat.C:
			c.SSEvent(service.TaskEventHeartbeat, service.TaskEvent{
				Version:    1,
				Kind:       service.TaskEventHeartbeat,
				Action:     "heartbeat",
				OccurredAt: now.UTC(),
			})
			c.Writer.Flush()
		}
	}
}
