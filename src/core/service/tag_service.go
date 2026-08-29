package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

var (
	errStaleTagGeneration = errors.New("标签生成任务已过期")
	errAutoTagDisabled    = errors.New("自动标签已关闭")
)

type globalTagRuntime struct {
	ruleSet  *models.TagRuleSet
	snapshot *tagging.Snapshot
}

type tagRebuildGuard struct {
	jobID    string
	runToken string
}

// TagService 是标签子系统的兼容门面。
// 具体职责分别位于 tag_runtime、tag_generation、tag_catalog、tag_query、
// tag_file_assignment、tag_directory、tag_rules 和各个 worker 文件中。
type TagService struct {
	factory          *impl.RepositoryFactory
	globalRuntime    atomic.Pointer[globalTagRuntime]
	runtimeMu        sync.Mutex
	autoEnabled      atomic.Bool
	autoLimit        atomic.Int64
	runtimeReady     chan struct{}
	runtimeReadyOnce sync.Once
	pendingWake      chan struct{}
	rebuildWake      chan struct{}
	metadataWake     chan struct{}
	ruleWake         chan struct{}
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	started          atomic.Bool
	degraded         atomic.Bool
	degradedReason   atomic.Value
	// rebuildBatchFailures 记录每个重建任务连续提交批次失败的次数，用于熔断。
	// 这是进程内的运行时保护，不需要持久化：服务重启后任务本就要重新评估。
	rebuildBatchFailures sync.Map
}

func NewTagService(factory *impl.RepositoryFactory) (*TagService, error) {
	ctx, cancel := context.WithCancel(context.Background())
	service := &TagService{
		factory:     factory,
		pendingWake: make(chan struct{}, 1), rebuildWake: make(chan struct{}, 1),
		metadataWake: make(chan struct{}, 1), ruleWake: make(chan struct{}, 1),
		runtimeReady: make(chan struct{}), ctx: ctx, cancel: cancel,
	}
	service.autoEnabled.Store(true)
	service.autoLimit.Store(tagging.DefaultAutoTagLimit)
	service.degradedReason.Store("")
	return service, nil
}

// initializeRuntime 只在规则轮询器首次加载时调用，负责解锁等待 runtimeReady 的
// 重建与自动标签 worker。这里的任何一步失败都必须降级而不是直接返回：一旦
// markRuntimeReady 没被调用，两个 worker 会永久阻塞且几乎没有任何日志。
func (s *TagService) initializeRuntime(ctx context.Context) error {
	if err := s.reloadSettings(ctx); err != nil {
		// 读取自动标签开关失败时退化为内置默认值，保证后续流程继续。
		logger.LOG.Error("加载自动标签设置失败，已使用内置默认值继续", "error", err)
	}
	if err := s.reloadGlobalRules(ctx, true); err != nil {
		if fallbackErr := s.installFallbackSnapshot(err); fallbackErr != nil {
			// 连内置基础规则都不可用时，仍然放开 worker：让文件级任务以失败的方式
			// 被记录和诊断，而不是让两个 worker 永久静默阻塞在 waitForRuntime。
			logger.LOG.Error("初始化标签规则运行时失败，标签任务将以失败方式继续", "error", fallbackErr)
			s.markRuntimeReady()
			return fallbackErr
		}
	}
	if s.globalRuntime.Load() == nil {
		s.markRuntimeReady()
	}
	return nil
}

func (s *TagService) markRuntimeReady() {
	if s == nil || s.runtimeReady == nil {
		return
	}
	s.runtimeReadyOnce.Do(func() { close(s.runtimeReady) })
}

func (s *TagService) waitForRuntime() bool {
	if s == nil {
		return false
	}
	if s.runtimeReady == nil {
		return s.globalRuntime.Load() != nil
	}
	select {
	case <-s.runtimeReady:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *TagService) installFallbackSnapshot(cause error) error {
	if s.globalRuntime.Load() != nil {
		return nil
	}
	now := time.Now()
	ruleSet := &models.TagRuleSet{
		ID: "builtin-fallback", Version: 0, Revision: 1, Status: models.TagRuleSetActive,
		CreatedBy: "system", CreatedAt: now, UpdatedAt: now,
	}
	snapshot, err := tagging.CompileSnapshot(*ruleSet, int(s.autoLimit.Load()))
	if err != nil {
		return fmt.Errorf("编译内置基础标签规则失败: %w", err)
	}
	s.runtimeMu.Lock()
	if s.globalRuntime.Load() != nil {
		s.runtimeMu.Unlock()
		return nil
	}
	s.globalRuntime.Store(&globalTagRuntime{ruleSet: ruleSet, snapshot: snapshot})
	s.degraded.Store(true)
	s.degradedReason.Store(cause.Error())
	s.runtimeMu.Unlock()
	s.markRuntimeReady()
	logger.LOG.Error("活动标签规则损坏，已启用内置基础规则", "error", cause)
	return nil
}

func (s *TagService) Start() {
	if !s.started.CompareAndSwap(false, true) {
		return
	}
	s.wg.Add(4)
	go s.runPendingWorker()
	go s.runRebuildWorker()
	go s.runRulePoller()
	go s.runMetadataWorker()
	s.notifyPending()
	s.notifyRebuild()
	s.notifyMetadata()
}

func (s *TagService) Close() {
	if s == nil {
		return
	}
	s.cancel()
	if s.started.Load() {
		s.wg.Wait()
	}
}

func (s *TagService) notify(signal chan struct{}) {
	if s == nil || signal == nil || (s.ctx != nil && s.ctx.Err() != nil) {
		return
	}
	select {
	case signal <- struct{}{}:
	default:
	}
}

func (s *TagService) notifyPending()  { s.notify(s.pendingWake) }
func (s *TagService) notifyRebuild()  { s.notify(s.rebuildWake) }
func (s *TagService) notifyMetadata() { s.notify(s.metadataWake) }
func (s *TagService) notifyRules()    { s.notify(s.ruleWake) }
