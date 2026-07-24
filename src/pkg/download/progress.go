package download

import "context"

// ProgressReporter 将下载进度交给任务管理器原子持久化并续租。
type ProgressReporter func(ctx context.Context, downloadedSize, speed int64, progress int) (bool, error)
