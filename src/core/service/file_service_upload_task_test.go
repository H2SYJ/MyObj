package service

import (
	"context"
	"io"
	"log/slog"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestUpdateUploadTaskSynchronizesTotalChunks(t *testing.T) {
	previousLogger := logger.LOG
	logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	defer func() { logger.LOG = previousLogger }()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.UploadTask{}); err != nil {
		t.Fatalf("创建上传任务表失败: %v", err)
	}

	factory := impl.NewRepositoryFactory(db)
	task := &models.UploadTask{
		ID:          "task-1",
		UserID:      "user-1",
		FileName:    "示例.bin",
		FileSize:    10,
		ChunkSize:   5,
		TotalChunks: 1,
		Status:      "pending",
	}
	if err := factory.UploadTask().Create(context.Background(), task); err != nil {
		t.Fatalf("创建上传任务失败: %v", err)
	}

	service := &FileService{factory: factory}
	if err := service.updateUploadTask(context.Background(), task.ID, task.UserID, 2, 2, "", "uploading", ""); err != nil {
		t.Fatalf("更新上传任务失败: %v", err)
	}

	updated, err := factory.UploadTask().GetByID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("查询上传任务失败: %v", err)
	}
	if updated.TotalChunks != 2 || updated.UploadedChunks != 2 {
		t.Fatalf("分片数量未同步: uploaded=%d total=%d", updated.UploadedChunks, updated.TotalChunks)
	}
}

func TestCalculateUploadTaskProgress(t *testing.T) {
	tests := []struct {
		name string
		task models.UploadTask
		want float64
	}{
		{
			name: "完成状态固定为百分之百",
			task: models.UploadTask{Status: "completed", UploadedChunks: 1, TotalChunks: 5},
			want: 100,
		},
		{
			name: "普通进度按分片计算",
			task: models.UploadTask{Status: "uploading", UploadedChunks: 1, TotalChunks: 4},
			want: 25,
		},
		{
			name: "异常进度限制为百分之百",
			task: models.UploadTask{Status: "uploading", UploadedChunks: 5, TotalChunks: 2},
			want: 100,
		},
		{
			name: "负进度限制为零",
			task: models.UploadTask{Status: "uploading", UploadedChunks: -1, TotalChunks: 2},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calculateUploadTaskProgress(&tt.task); got != tt.want {
				t.Fatalf("进度计算错误: got=%v want=%v", got, tt.want)
			}
		})
	}
}
