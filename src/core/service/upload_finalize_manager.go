package service

import (
	"context"
	"fmt"
	"myobj/src/core/domain/request"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/upload"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type uploadFinalizeJob struct {
	taskID       string
	filePassword string
}

// UploadFinalizeManager 串行执行磁盘密集型文件处理，避免阻塞最后一个分片请求。
type UploadFinalizeManager struct {
	service *FileService
	queue   chan uploadFinalizeJob
	running sync.Map
	once    sync.Once
}

func newUploadFinalizeManager(service *FileService) *UploadFinalizeManager {
	return &UploadFinalizeManager{
		service: service,
		queue:   make(chan uploadFinalizeJob, 128),
	}
}

func (m *UploadFinalizeManager) Start() {
	m.once.Do(func() {
		go m.worker()
		m.recoverProcessingTasks()
	})
}

func (m *UploadFinalizeManager) Enqueue(taskID, filePassword string) bool {
	if _, loaded := m.running.LoadOrStore(taskID, struct{}{}); loaded {
		return false
	}
	job := uploadFinalizeJob{taskID: taskID, filePassword: filePassword}
	select {
	case m.queue <- job:
	default:
		// 队列已满时不阻塞最后一个分片请求；任务状态已持久化，进程重启后也能恢复。
		go func() { m.queue <- job }()
	}
	return true
}

func (m *UploadFinalizeManager) enqueueAfterCurrent(taskID, filePassword string) {
	go func() {
		for {
			if _, running := m.running.Load(taskID); !running {
				m.Enqueue(taskID, filePassword)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
}

func (m *UploadFinalizeManager) worker() {
	for job := range m.queue {
		func() {
			defer m.running.Delete(job.taskID)
			defer func() {
				if recovered := recover(); recovered != nil {
					cause := fmt.Errorf("后台处理发生异常: %v", recovered)
					if task, err := m.service.factory.UploadTask().GetByID(context.Background(), job.taskID); err == nil {
						_ = m.service.failFinalizeTask(context.Background(), task, cause)
					}
					logger.LOG.Error("后台处理上传文件发生异常", "taskID", job.taskID, "error", cause)
				}
			}()
			if err := m.service.finalizeUploadTask(context.Background(), job); err != nil {
				logger.LOG.Error("后台处理上传文件失败", "taskID", job.taskID, "error", err)
			}
		}()
	}
}

func (m *UploadFinalizeManager) recoverProcessingTasks() {
	tasks, err := m.service.factory.UploadTask().ListByStatus(context.Background(), "processing")
	if err != nil {
		logger.LOG.Error("查询待恢复上传任务失败", "error", err)
		return
	}
	for _, task := range tasks {
		if task.IsEnc {
			task.Status = "failed"
			task.ProcessingStage = ""
			task.ErrorMessage = "服务重启后加密任务需要重新输入文件密码"
			task.UpdateTime = custom_type.Now()
			if err := m.service.factory.UploadTask().Update(context.Background(), task); err != nil {
				logger.LOG.Error("标记加密恢复任务失败", "taskID", task.ID, "error", err)
			}
			continue
		}
		if err := validateTaskChunks(task); err != nil {
			task.Status = "failed"
			task.ProcessingStage = ""
			task.ErrorMessage = err.Error()
			task.UpdateTime = custom_type.Now()
			_ = m.service.factory.UploadTask().Update(context.Background(), task)
			continue
		}
		m.Enqueue(task.ID, "")
	}
}

func validateTaskChunks(task *models.UploadTask) error {
	if task.TempDir == "" || task.TotalChunks <= 0 {
		return fmt.Errorf("上传任务缺少可恢复的临时分片信息")
	}
	var totalSize int64
	for index := 0; index < task.TotalChunks; index++ {
		info, err := os.Stat(filepath.Join(task.TempDir, fmt.Sprintf("%d.chunk.data", index)))
		if err != nil {
			return fmt.Errorf("上传任务缺少第 %d 个分片", index)
		}
		totalSize += info.Size()
	}
	if totalSize != task.FileSize {
		return fmt.Errorf("上传分片总大小不一致: 实际=%d, 期望=%d", totalSize, task.FileSize)
	}
	return nil
}

func (f *FileService) finalizeUploadTask(ctx context.Context, job uploadFinalizeJob) error {
	task, err := f.factory.UploadTask().GetByID(ctx, job.taskID)
	if err != nil {
		return err
	}
	if task.Status != "processing" {
		return nil
	}
	if err := validateTaskChunks(task); err != nil {
		return f.failFinalizeTask(ctx, task, err)
	}
	if task.IsEnc && job.filePassword == "" {
		return f.failFinalizeTask(ctx, task, fmt.Errorf("加密任务必须重新输入文件密码"))
	}

	task.ProcessingStage = "validating"
	task.UpdateTime = custom_type.Now()
	if err := f.factory.UploadTask().Update(ctx, task); err != nil {
		return err
	}

	tempThumbnailPath := upload.TempVideoThumbnailPath(task.TempDir)
	if _, err := os.Stat(tempThumbnailPath); err != nil {
		tempThumbnailPath = ""
	}
	task.ProcessingStage = "storing"
	if task.IsEnc {
		task.ProcessingStage = "encrypting"
	}
	task.UpdateTime = custom_type.Now()
	if err := f.factory.UploadTask().Update(ctx, task); err != nil {
		return err
	}

	data := &upload.FileUploadData{
		TempFilePath:        filepath.Join(task.TempDir, "0.chunk.data"),
		TempThumbnailPath:   tempThumbnailPath,
		FileName:            task.FileName,
		FileSize:            task.FileSize,
		ChunkSignature:      task.ChunkSignature,
		FirstChunkHash:      task.FirstChunkHash,
		SecondChunkHash:     task.SecondChunkHash,
		ThirdChunkHash:      task.ThirdChunkHash,
		IsEnc:               task.IsEnc,
		IsChunk:             true,
		ChunkCount:          task.TotalChunks,
		VirtualPath:         task.PathID,
		UserID:              task.UserID,
		DiskID:              task.DiskID,
		FilePassword:        job.filePassword,
		PreserveTempOnError: true,
		StageCallback: func(stage string) {
			task.ProcessingStage = stage
			task.UpdateTime = custom_type.Now()
			if updateErr := f.factory.UploadTask().Update(context.Background(), task); updateErr != nil {
				logger.LOG.Warn("更新后台处理阶段失败", "taskID", task.ID, "stage", stage, "error", updateErr)
			}
		},
	}

	fileID, err := upload.ProcessUploadedFile(data, f.factory)
	if err != nil {
		return f.failFinalizeTask(ctx, task, err)
	}

	task.Status = "completed"
	task.ProcessingStage = "completed"
	task.ResultFileID = fileID
	task.ErrorMessage = ""
	task.UpdateTime = custom_type.Now()
	if err := f.factory.UploadTask().Update(ctx, task); err != nil {
		return err
	}
	_ = f.cacheLocal.Delete(fmt.Sprintf("fileUpload:%s", task.ID))
	_ = f.cacheLocal.Delete(fmt.Sprintf("fileUploadReq:%s", task.ID))
	logger.LOG.Info("后台处理上传文件完成", "taskID", task.ID, "fileID", fileID)
	return nil
}

func (f *FileService) failFinalizeTask(ctx context.Context, task *models.UploadTask, cause error) error {
	task.Status = "failed"
	task.ProcessingStage = ""
	task.ErrorMessage = cause.Error()
	task.UpdateTime = custom_type.Now()
	if err := f.factory.UploadTask().Update(ctx, task); err != nil {
		return fmt.Errorf("%v；更新任务失败: %w", cause, err)
	}
	return cause
}

// RetryUploadFinalize 重新处理已失败且分片仍完整的任务。
func (f *FileService) RetryUploadFinalize(req *request.RetryUploadFinalizeRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()
	task, err := f.factory.UploadTask().GetByID(ctx, req.PrecheckID)
	if err != nil {
		return nil, err
	}
	if task.UserID != userID {
		return models.NewJsonResponse(403, "无权处理该上传任务", nil), nil
	}
	if task.Status != "failed" {
		return models.NewJsonResponse(409, "只有失败任务可以重新处理", nil), nil
	}
	if task.IsEnc && req.FilePassword == "" {
		return models.NewJsonResponse(400, "加密任务必须提供文件密码", nil), nil
	}
	if err := validateTaskChunks(task); err != nil {
		return models.NewJsonResponse(400, err.Error(), nil), nil
	}
	claimed, err := f.factory.UploadTask().ClaimProcessing(ctx, task.ID, []string{"failed"})
	if err != nil {
		return nil, err
	}
	if !claimed {
		return models.NewJsonResponse(409, "任务状态已发生变化", nil), nil
	}
	if !f.finalizeManager.Enqueue(task.ID, req.FilePassword) {
		f.finalizeManager.enqueueAfterCurrent(task.ID, req.FilePassword)
	}
	return models.NewJsonResponse(200, "文件已重新进入处理队列", map[string]interface{}{
		"task_id": task.ID,
		"status":  "processing",
	}), nil
}
