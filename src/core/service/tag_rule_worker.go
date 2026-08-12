package service

import (
	"time"

	"myobj/src/pkg/logger"
)

const tagRuleReconcileInterval = 60 * time.Second

// 本文件负责刷新全局规则和自动标签设置。
func (s *TagService) runRulePoller() {
	defer s.wg.Done()
	timer := time.NewTimer(0)
	defer timer.Stop()
	runReload := func() {
		err := s.reloadRuleRuntime()
		delay := tagRuleReconcileInterval
		if err != nil {
			s.degraded.Store(true)
			s.degradedReason.Store(err.Error())
			logger.LOG.Error("刷新标签规则运行时失败，将继续重试", "worker", "rules", "error", err, "retry_after", schedulerErrorRetry)
			delay = schedulerErrorRetry
		}
		resetSchedulerTimer(timer, delay)
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.ruleWake:
			runReload()
		case <-timer.C:
			runReload()
		}
	}
}

func (s *TagService) reloadRuleRuntime() error {
	wasEnabled := s.autoEnabled.Load()
	if s.globalRuntime.Load() == nil {
		if err := s.initializeRuntime(s.ctx); err != nil {
			return err
		}
	} else {
		if err := s.reloadSettings(s.ctx); err != nil {
			return err
		}
		if err := s.reloadGlobalRules(s.ctx, false); err != nil {
			return err
		}
	}
	if !wasEnabled && s.autoEnabled.Load() {
		s.notifyPending()
		s.notifyRebuild()
	}
	return nil
}
