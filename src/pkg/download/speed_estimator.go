package download

import (
	"context"
	"math"
	"sync"
	"time"
)

const (
	speedSampleInterval = time.Second
	speedTimeConstant   = 3 * time.Second
	speedIdleSamples    = 2
)

// speedEstimator 使用按真实时间加权的指数移动平均计算任务速度。
// 该类型不保证并发安全，应由单个采样协程持有。
type speedEstimator struct {
	timeConstant time.Duration
	idleSamples  int

	lastBytes   int64
	lastUpdate  time.Time
	smoothed    float64
	zeroCount   int
	initialized bool
	hasSpeed    bool
}

func newSpeedEstimator(timeConstant time.Duration, idleSamples int) *speedEstimator {
	if timeConstant <= 0 {
		timeConstant = speedTimeConstant
	}
	if idleSamples <= 0 {
		idleSamples = speedIdleSamples
	}
	return &speedEstimator{timeConstant: timeConstant, idleSamples: idleSamples}
}

// Reset 立即建立累计字节基线，历史字节不会进入下一次速度样本。
func (e *speedEstimator) Reset(totalBytes int64, now time.Time) {
	if totalBytes < 0 {
		totalBytes = 0
	}
	e.lastBytes = totalBytes
	e.lastUpdate = now
	e.smoothed = 0
	e.zeroCount = 0
	e.initialized = true
	e.hasSpeed = false
}

// Sample 根据累计字节数返回平滑后的字节每秒。
func (e *speedEstimator) Sample(totalBytes int64, now time.Time) int64 {
	if totalBytes < 0 {
		totalBytes = 0
	}
	if !e.initialized {
		e.Reset(totalBytes, now)
		return 0
	}

	elapsed := now.Sub(e.lastUpdate)
	if elapsed <= 0 {
		return roundedSpeed(e.smoothed)
	}
	delta := totalBytes - e.lastBytes
	e.lastBytes = totalBytes
	e.lastUpdate = now
	if delta < 0 {
		e.smoothed = 0
		e.zeroCount = 0
		e.hasSpeed = false
		return 0
	}

	rawSpeed := float64(delta) / elapsed.Seconds()
	if delta == 0 {
		e.zeroCount++
		if e.zeroCount >= e.idleSamples {
			e.smoothed = 0
			e.hasSpeed = false
			return 0
		}
	} else {
		e.zeroCount = 0
	}

	if !e.hasSpeed {
		if rawSpeed <= 0 {
			return 0
		}
		e.smoothed = rawSpeed
		e.hasSpeed = true
		return roundedSpeed(e.smoothed)
	}

	alpha := 1 - math.Exp(-elapsed.Seconds()/e.timeConstant.Seconds())
	e.smoothed += alpha * (rawSpeed - e.smoothed)
	return roundedSpeed(e.smoothed)
}

func roundedSpeed(speed float64) int64 {
	if math.IsNaN(speed) || speed <= 0 {
		return 0
	}
	if math.IsInf(speed, 1) || speed >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(math.Round(speed))
}

// speedSampler 固定周期读取单调累计字节并上报平滑速度。
type speedSampler struct {
	ctx      context.Context
	cancel   context.CancelFunc
	interval time.Duration
	bytes    func() int64
	report   func(speed int64) error

	estimator *speedEstimator
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
	errMu     sync.Mutex
	err       error
}

func startSpeedSampler(
	ctx context.Context,
	cancel context.CancelFunc,
	interval time.Duration,
	bytes func() int64,
	report func(speed int64) error,
) *speedSampler {
	if ctx == nil {
		ctx = context.Background()
	}
	if interval <= 0 {
		interval = speedSampleInterval
	}
	sampler := &speedSampler{
		ctx: ctx, cancel: cancel, interval: interval, bytes: bytes, report: report,
		estimator: newSpeedEstimator(speedTimeConstant, speedIdleSamples),
		stop:      make(chan struct{}), done: make(chan struct{}),
	}
	sampler.estimator.Reset(bytes(), time.Now())
	go sampler.run()
	return sampler
}

func (s *speedSampler) run() {
	defer close(s.done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
			speed := s.estimator.Sample(s.bytes(), time.Now())
			if err := s.report(speed); err != nil {
				s.errMu.Lock()
				s.err = err
				s.errMu.Unlock()
				if s.cancel != nil {
					s.cancel()
				}
				return
			}
		}
	}
}

// Stop 等待采样协程彻底退出，返回首次上报错误。
func (s *speedSampler) Stop() error {
	if s == nil {
		return nil
	}
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.done
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}
