package download

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"
)

func TestSpeedEstimatorConstantSpeedAndVariableIntervals(t *testing.T) {
	estimator := newSpeedEstimator(3*time.Second, 2)
	start := time.Unix(0, 0)
	estimator.Reset(0, start)

	if got := estimator.Sample(1024, start.Add(time.Second)); got != 1024 {
		t.Fatalf("首个速度=%d，期望1024", got)
	}
	if got := estimator.Sample(2560, start.Add(2500*time.Millisecond)); got != 1024 {
		t.Fatalf("可变间隔下的恒定速度=%d，期望1024", got)
	}
}

func TestSpeedEstimatorSmoothsBurstAndResetsAfterIdle(t *testing.T) {
	estimator := newSpeedEstimator(3*time.Second, 2)
	start := time.Unix(0, 0)
	estimator.Reset(0, start)
	if got := estimator.Sample(1000, start.Add(time.Second)); got != 1000 {
		t.Fatalf("初始速度=%d，期望1000", got)
	}

	alpha := 1 - math.Exp(-1.0/3.0)
	wantBurst := int64(math.Round(1000 + alpha*(9000-1000)))
	if got := estimator.Sample(10000, start.Add(2*time.Second)); got != wantBurst {
		t.Fatalf("突发后的平滑速度=%d，期望%d", got, wantBurst)
	}
	firstIdle := estimator.Sample(10000, start.Add(3*time.Second))
	if firstIdle <= 0 || firstIdle >= wantBurst {
		t.Fatalf("单次空闲应衰减但不归零，实际%d", firstIdle)
	}
	if got := estimator.Sample(10000, start.Add(4*time.Second)); got != 0 {
		t.Fatalf("连续两次空闲后的速度=%d，期望0", got)
	}
	if got := estimator.Sample(11000, start.Add(5*time.Second)); got != 1000 {
		t.Fatalf("空闲恢复后的首个速度=%d，期望1000", got)
	}
}

func TestSpeedEstimatorRebasesDecreasingAndInvalidCounters(t *testing.T) {
	estimator := newSpeedEstimator(3*time.Second, 2)
	start := time.Unix(0, 0)
	estimator.Reset(500, start)
	if got := estimator.Sample(600, start.Add(time.Second)); got != 100 {
		t.Fatalf("恢复基线后的速度=%d，期望100", got)
	}
	if got := estimator.Sample(400, start.Add(2*time.Second)); got != 0 {
		t.Fatalf("计数倒退后的速度=%d，期望0", got)
	}
	if got := estimator.Sample(500, start.Add(3*time.Second)); got != 100 {
		t.Fatalf("重新建立基线后的速度=%d，期望100", got)
	}
	if got := roundedSpeed(math.Inf(1)); got != math.MaxInt64 {
		t.Fatalf("无穷速度边界=%d，期望%d", got, int64(math.MaxInt64))
	}
	if got := roundedSpeed(math.NaN()); got != 0 {
		t.Fatalf("NaN速度=%d，期望0", got)
	}
}

func TestSpeedSamplerStopsAndPropagatesReporterError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var transferred atomic.Int64
	wantErr := errors.New("进度上报失败")
	sampler := startSpeedSampler(ctx, cancel, 10*time.Millisecond, transferred.Load, func(int64) error {
		return wantErr
	})
	transferred.Store(1024)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("上报错误未取消下载上下文")
	}
	if err := sampler.Stop(); !errors.Is(err, wantErr) {
		t.Fatalf("采样器错误=%v，期望%v", err, wantErr)
	}
	if err := sampler.Stop(); !errors.Is(err, wantErr) {
		t.Fatalf("重复停止后的错误=%v，期望%v", err, wantErr)
	}
}

func TestSpeedSamplerDoesNotReportAfterStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var transferred atomic.Int64
	var reports atomic.Int32
	sampler := startSpeedSampler(ctx, cancel, 10*time.Millisecond, transferred.Load, func(int64) error {
		reports.Add(1)
		return nil
	})
	transferred.Store(1024)
	deadline := time.Now().Add(time.Second)
	for reports.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if reports.Load() == 0 {
		t.Fatal("采样器未产生周期上报")
	}
	if err := sampler.Stop(); err != nil {
		t.Fatal(err)
	}
	stoppedReports := reports.Load()
	transferred.Add(1024)
	time.Sleep(30 * time.Millisecond)
	if got := reports.Load(); got != stoppedReports {
		t.Fatalf("停止后仍产生进度上报: before=%d after=%d", stoppedReports, got)
	}
}
