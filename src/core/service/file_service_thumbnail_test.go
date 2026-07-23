package service

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"mime/multipart"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupThumbnailService(t *testing.T, encrypted bool) (*FileService, *models.FileInfo, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("连接测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.FileInfo{}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	// UserFiles 的生产模型将软删除列标记为 NOT NULL，但 GORM 的未删除值是 NULL。
	// 测试表按软删除的实际查询语义创建，避免 SQLite 拒绝正常记录。
	if err := db.Exec(`CREATE TABLE user_files (
		user_id TEXT NOT NULL,
		file_id TEXT NOT NULL,
		file_name TEXT NOT NULL,
		directory_id INTEGER NOT NULL,
		public BOOLEAN NOT NULL,
		created_at DATETIME NOT NULL,
		deleted_at DATETIME,
		uf_id TEXT NOT NULL
	)`).Error; err != nil {
		t.Fatalf("创建用户文件测试表失败: %v", err)
	}

	storageDir := t.TempDir()
	mainPath := filepath.Join(storageDir, "random-file.data")
	if err := os.WriteFile(mainPath, []byte("video"), 0644); err != nil {
		t.Fatalf("创建主文件失败: %v", err)
	}
	fileInfo := &models.FileInfo{
		ID:         "file-info-1",
		Name:       "示例视频.mp4",
		RandomName: "random-file",
		Size:       5,
		Mime:       "video/mp4",
		Path:       mainPath,
		FileHash:   "hash",
		IsEnc:      encrypted,
		IsChunk:    false,
		CreatedAt:  custom_type.Now(),
		UpdatedAt:  custom_type.Now(),
	}
	if err := db.Create(fileInfo).Error; err != nil {
		t.Fatalf("创建文件信息失败: %v", err)
	}
	userFile := &models.UserFiles{
		UserID:      "user-1",
		FileID:      fileInfo.ID,
		FileName:    fileInfo.Name,
		DirectoryID: 1,
		UfID:        "user-file-1",
		CreatedAt:   custom_type.Now(),
	}
	if err := db.Create(userFile).Error; err != nil {
		t.Fatalf("创建用户文件失败: %v", err)
	}

	logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewFileService(impl.NewRepositoryFactory(db), nil), fileInfo, storageDir
}

func createThumbnailUpload(t *testing.T, width, height int) (*os.File, *multipart.FileHeader) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "缩略图.jpg")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建缩略图失败: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.White)
	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 90}); err != nil {
		file.Close()
		t.Fatalf("编码缩略图失败: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("关闭缩略图失败: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取缩略图信息失败: %v", err)
	}
	file, err = os.Open(path)
	if err != nil {
		t.Fatalf("打开缩略图失败: %v", err)
	}
	return file, &multipart.FileHeader{Filename: filepath.Base(path), Size: info.Size()}
}

func TestUpdateThumbnailReplacesFileAndUpdatesPath(t *testing.T) {
	service, fileInfo, storageDir := setupThumbnailService(t, false)
	oldThumbnailPath := filepath.Join(storageDir, fileInfo.RandomName+".jpg")
	if err := os.WriteFile(oldThumbnailPath, []byte("old-thumbnail"), 0644); err != nil {
		t.Fatalf("创建旧缩略图失败: %v", err)
	}
	if err := service.factory.FileInfo().UpdateThumbnailPath(context.Background(), fileInfo.ID, oldThumbnailPath); err != nil {
		t.Fatalf("记录旧缩略图失败: %v", err)
	}

	thumbnail, header := createThumbnailUpload(t, 300, 200)
	defer thumbnail.Close()
	result, err := service.UpdateThumbnail(
		context.Background(), "user-file-1", "user-1", thumbnail, header,
	)
	if err != nil {
		t.Fatalf("修改缩略图失败: %v", err)
	}
	if result.Code != 200 {
		t.Fatalf("修改缩略图响应码错误: got=%d want=200", result.Code)
	}

	updated, err := service.factory.FileInfo().GetByID(context.Background(), fileInfo.ID)
	if err != nil {
		t.Fatalf("查询更新后的文件失败: %v", err)
	}
	if updated.ThumbnailImg != oldThumbnailPath {
		t.Fatalf("缩略图路径错误: got=%s want=%s", updated.ThumbnailImg, oldThumbnailPath)
	}
	saved, err := os.Open(updated.ThumbnailImg)
	if err != nil {
		t.Fatalf("打开新缩略图失败: %v", err)
	}
	defer saved.Close()
	config, err := jpeg.DecodeConfig(saved)
	if err != nil {
		t.Fatalf("新缩略图不是有效JPEG: %v", err)
	}
	if config.Width != 300 || config.Height != 200 {
		t.Fatalf("新缩略图尺寸错误: got=%dx%d", config.Width, config.Height)
	}
}

func TestUpdateThumbnailRejectsUnauthorizedAndEncryptedFiles(t *testing.T) {
	service, _, _ := setupThumbnailService(t, false)
	thumbnail, header := createThumbnailUpload(t, 100, 100)
	result, err := service.UpdateThumbnail(
		context.Background(), "user-file-1", "other-user", thumbnail, header,
	)
	thumbnail.Close()
	if err != nil {
		t.Fatalf("无权访问应返回业务错误: %v", err)
	}
	if result.Code != 404 {
		t.Fatalf("无权访问响应码错误: got=%d want=404", result.Code)
	}

	encryptedService, _, _ := setupThumbnailService(t, true)
	thumbnail, header = createThumbnailUpload(t, 100, 100)
	defer thumbnail.Close()
	result, err = encryptedService.UpdateThumbnail(
		context.Background(), "user-file-1", "user-1", thumbnail, header,
	)
	if err != nil {
		t.Fatalf("加密文件应返回业务错误: %v", err)
	}
	if result.Code != 403 {
		t.Fatalf("加密文件响应码错误: got=%d want=403", result.Code)
	}
}

func TestReplaceThumbnailAndUpdateRestoresOldFileOnDatabaseFailure(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "thumbnail.jpg")
	stagedPath := filepath.Join(tempDir, "staged.jpg")
	if err := os.WriteFile(targetPath, []byte("old"), 0644); err != nil {
		t.Fatalf("创建旧缩略图失败: %v", err)
	}
	if err := os.WriteFile(stagedPath, []byte("new"), 0644); err != nil {
		t.Fatalf("创建新缩略图失败: %v", err)
	}

	err := replaceThumbnailAndUpdate(stagedPath, targetPath, func() error {
		return gorm.ErrInvalidDB
	})
	if err == nil {
		t.Fatal("数据库更新失败时应返回错误")
	}
	content, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatalf("读取回滚后的缩略图失败: %v", readErr)
	}
	if string(content) != "old" {
		t.Fatalf("旧缩略图未恢复: got=%q", content)
	}
}
