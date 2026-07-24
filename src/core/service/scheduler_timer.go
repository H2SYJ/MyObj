package service

import "time"

const (
	schedulerFallbackInterval = 30 * time.Second
	schedulerErrorRetry       = 5 * time.Second
)

func resetSchedulerTimer(timer *time.Timer, delay time.Duration) {
	if delay < 0 {
		delay = 0
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}

func schedulerWakeDelay(now time.Time, next *time.Time) time.Duration {
	if next == nil {
		return schedulerFallbackInterval
	}
	delay := next.Sub(now)
	if delay < 0 {
		return 0
	}
	if delay > schedulerFallbackInterval {
		return schedulerFallbackInterval
	}
	return delay
}
