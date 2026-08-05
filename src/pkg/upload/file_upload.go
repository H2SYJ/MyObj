package upload

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"myobj/src/config"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/hash"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/metadata"
	"myobj/src/pkg/models"
	"myobj/src/pkg/preview"
	"myobj/src/pkg/tagging"
	"myobj/src/pkg/util"
	"myobj/src/pkg/virtualpath"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/zeebo/blake3"
	"gorm.io/gorm"
)

// FileUploadData 文件上传参数
type FileUploadData struct {
	// 临时文件路径
	TempFilePath string `json:"temp_file_path"`
	// 临时视频缩略图路径
	TempThumbnailPath string `json:"temp_thumbnail_path"`
	// 文件名
	FileName string `json:"file_name"`
	// 文件大小
	FileSize int64 `json:"file_size"`
	// 文件hash签名
	ChunkSignature string `json:"chunk_signature"`
	// 文件分片hash 第一
	FirstChunkHash string `json:"first_chunk_hash"`
	// 文件分片hash 第二
	SecondChunkHash string `json:"second_chunk_hash"`
	// 文件分片hash 第三
	ThirdChunkHash string `json:"third_chunk_hash"`
	// 是否需要加密
	IsEnc bool `json:"is_enc"`
	// 是否分块上传
	IsChunk bool `json:"is_chunk"`
	// 分块数量
	ChunkCount int `json:"chunk_count"`
	// DirectoryID 用于用户主动上传；SavePath 用于下载任务按绝对路径入库。
	DirectoryID int    `json:"directory_id"`
	SavePath    string `json:"save_path"`
	// 上传用户ID
	UserID string `json:"user_id"`
	// 预检阶段选中的存储磁盘ID
	DiskID string `json:"disk_id"`
	// 文件加密密码（明文）
	FilePassword string `json:"file_password"`
	// ReservedSize 离线下载预先扣减的用户空间
	ReservedSize int64 `json:"reserved_size"`
	// 处理失败时保留临时分片，供后台任务重试。
	PreserveTempOnError bool `json:"preserve_temp_on_error"`
	// PreserveTempOnSuccess 成功后保留源文件，由批次任务统一清理。
	PreserveTempOnSuccess bool `json:"preserve_temp_on_success"`
	// StageCallback 用于异步任务记录处理阶段，不参与持久化。
	StageCallback func(stage string) `json:"-"`
}

// ProcessUploadedFile 处理已上传的文件
// 参数:
//   - data: 文件上传数据
//   - repoFactory: 数据库仓储工厂
//
// 返回:
//   - fileID: 生成的文件ID
//   - err: 错误信息
func ProcessUploadedFile(data *FileUploadData, repoFactory *impl.RepositoryFactory) (fileID string, err error) {
	ctx := context.Background()

	// 调试：检查初始临时文件
	if tempInfo, err := os.Stat(data.TempFilePath); err == nil {
		logger.LOG.Debug("开始处理文件", "TempFilePath", data.TempFilePath, "临时文件大小", tempInfo.Size(), "期望大小", data.FileSize)
	} else {
		return "", fmt.Errorf("临时文件不存在: %s, %w", data.TempFilePath, err)
	}

	// 确保无论成功失败都清理临时文件
	defer func() {
		if recovered := recover(); recovered != nil {
			if !data.PreserveTempOnError {
				cleanupTempFiles(data)
			}
			panic(recovered)
		}
		if (err == nil && !data.PreserveTempOnSuccess) || (err != nil && !data.PreserveTempOnError) {
			cleanupTempFiles(data)
		}
	}()

	// 1. 合并分片（如果是分片上传）
	mergedFilePath := data.TempFilePath
	var mergedFullHash string
	if data.IsChunk {
		mergedPath, fullHash, err := mergeChunks(data)
		if err != nil {
			return "", fmt.Errorf("合并分片失败: %w", err)
		}
		mergedFilePath = mergedPath
		mergedFullHash = fullHash
	}

	// 2. 检测文件MIME类型
	mimeType, err := detectMimeType(mergedFilePath)
	if err != nil {
		return "", fmt.Errorf("检测文件类型失败: %w", err)
	}

	// 3. 并行计算全量hash和生成缩略图（如果需要）
	type asyncResult struct {
		fullHash      string
		thumbnailPath string
		err           error
	}
	resultChan := make(chan asyncResult, 2)
	var wg sync.WaitGroup

	// 3.1 分片合并时已经顺便计算全量哈希；直传文件仍在这里计算。
	if mergedFullHash == "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hasher := hash.NewFastBlake3Hasher()
			fullHash, _, err := hasher.ComputeFileHash(mergedFilePath)
			resultChan <- asyncResult{fullHash: fullHash, err: err}
		}()
	}

	// 3.2 异步生成缩略图（如果需要）
	var needThumbnail bool
	var providedThumbnailPath string
	if config.CONFIG.File.Thumbnail && isImage(mimeType) {
		needThumbnail = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 临时缩略图路径
			tempThumbnail := mergedFilePath + ".thumbnail.jpg"
			err := preview.GenerateImageThumbnail(mergedFilePath, tempThumbnail, 300)
			resultChan <- asyncResult{thumbnailPath: tempThumbnail, err: err}
		}()
	} else if config.CONFIG.File.Thumbnail && isVideo(mimeType) && !data.IsEnc && data.TempThumbnailPath != "" {
		if _, err := os.Stat(data.TempThumbnailPath); err != nil {
			logger.LOG.Warn("视频缩略图不存在，继续处理原视频", "path", data.TempThumbnailPath, "error", err)
		} else {
			needThumbnail = true
			providedThumbnailPath = data.TempThumbnailPath
		}
	}

	// 等待异步任务完成
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// 收集结果
	fullHash, tempThumbnailPath := mergedFullHash, ""
	for result := range resultChan {
		if result.err != nil {
			return "", fmt.Errorf("异步处理失败: %w", result.err)
		}
		if result.fullHash != "" {
			fullHash = result.fullHash
		}
		if result.thumbnailPath != "" {
			tempThumbnailPath = result.thumbnailPath
		}
	}
	if tempThumbnailPath == "" {
		tempThumbnailPath = providedThumbnailPath
	}
	// 元数据必须在加密和分块存储前探测；Provider 失败只会让标签状态降级为 partial。
	metadataResult := metadata.Extract(ctx, metadata.Input{
		Path: mergedFilePath, FileName: data.FileName, MIME: mimeType,
		Size: data.FileSize, Encrypted: data.IsEnc,
	})
	if metadataResult.Partial {
		logger.LOG.Warn("文件元数据仅部分提取成功", "file_name", data.FileName, "error", metadataResult.ErrorText())
	}

	// 4. 使用预检阶段选中的存储磁盘，确保临时文件和最终文件位于同一磁盘。
	var disk *models.Disk
	if data.DiskID != "" {
		disk, err = repoFactory.Disk().GetByID(ctx, data.DiskID)
	} else {
		// 兼容未携带磁盘ID的旧调用方。
		disk, err = SelectBestDisk(ctx, repoFactory, data.FileSize)
	}
	if err != nil {
		return "", fmt.Errorf("获取存储磁盘失败: %w", err)
	}

	// 5. 生成文件ID和存储路径
	fileID = uuid.Must(uuid.NewV7()).String()
	virtualFileName := util.GenerateUniqueFilename()
	fileNameWithoutExt := strings.TrimSuffix(data.FileName, filepath.Ext(data.FileName))

	// 存储目录: {DataPath}/data/{原文件名不带后缀}/
	storageDir := filepath.Join(disk.DataPath, "data", fileNameWithoutExt)
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return "", fmt.Errorf("创建存储目录失败: %w", err)
	}

	// 6. 判断是否需要分片存储（超大文件）
	threshold := int64(config.CONFIG.File.BigFileThreshold) * 1024 * 1024 * 1024 // GB转字节
	needChunkStorage := data.FileSize > threshold

	// 7. 文件加密（如果需要）
	var finalFilePath string
	var fileEncHash string
	if data.IsEnc {
		reportUploadStage(data, "encrypting")
		// 验证用户是否提供了加密密码
		if data.FilePassword == "" {
			return "", fmt.Errorf("加密文件必须提供密码")
		}

		// 使用PBKDF2从明文密码和用户ID派生加密密钥
		// 这确保相同的密码+用户ID总是生成相同的密钥
		encryptionKey := util.DeriveEncryptionKey(data.FilePassword, data.UserID)
		logger.LOG.Debug("派生加密密钥", "userID", data.UserID, "keyLength", len(encryptionKey))

		// 加密文件
		encryptedPath := mergedFilePath + ".enc"
		crypto := util.NewFileCrypto(encryptionKey)
		if err := crypto.EncryptFile(mergedFilePath, encryptedPath); err != nil {
			return "", fmt.Errorf("文件加密失败: %w", err)
		}

		// 计算加密文件的hash
		encHasher := hash.NewFastBlake3Hasher()
		fileEncHash, _, err = encHasher.ComputeFileHash(encryptedPath)
		if err != nil {
			return "", fmt.Errorf("计算加密文件hash失败: %w", err)
		}
		logger.LOG.Debug("加密文件hash计算完成", "fileEncHash", fileEncHash)

		finalFilePath = encryptedPath
		// 加密后的临时文件会在cleanupTempFiles中一并清理（整个临时目录）
	} else {
		finalFilePath = mergedFilePath
	}

	// 8. 存储文件（根据是否需要分片）
	var chunks []*models.FileChunk
	var mainFilePath string
	var actualFileSize int64 // 实际文件大小

	if needChunkStorage {
		// 超大文件分片存储
		chunks, mainFilePath, err = splitAndStoreFile(finalFilePath, storageDir, virtualFileName, fileID, config.CONFIG.File.BigChunkSize)
		if err != nil {
			return "", fmt.Errorf("分片存储失败: %w", err)
		}
		// 计算实际文件大小（所有分片的总和）
		for _, chunk := range chunks {
			actualFileSize += int64(chunk.ChunkSize)
		}
	} else {
		// 普通文件直接存储
		mainFilePath = filepath.Join(storageDir, virtualFileName+".data")

		// 记录源文件大小用于调试
		srcInfo, err := os.Stat(finalFilePath)
		if err != nil {
			return "", fmt.Errorf("获取源文件信息失败: %w", err)
		}
		actualFileSize = srcInfo.Size() // 使用实际文件大小
		logger.LOG.Debug("准备复制文件", "源文件", finalFilePath, "目标文件", mainFilePath, "源文件大小", srcInfo.Size())

		// 临时目录和最终目录都位于预检选中的同一磁盘，重命名可避免再次复制整个文件。
		if err := os.Rename(finalFilePath, mainFilePath); err != nil {
			logger.LOG.Warn("同盘移动文件失败，回退到流式复制", "error", err)
			if copyErr := copyFile(finalFilePath, mainFilePath); copyErr != nil {
				return "", fmt.Errorf("存储文件失败: %w", copyErr)
			}
		}

		// 验证复制后的文件大小
		dstInfo, err := os.Stat(mainFilePath)
		if err != nil {
			return "", fmt.Errorf("获取目标文件信息失败: %w", err)
		}
		logger.LOG.Debug("文件复制完成", "目标文件大小", dstInfo.Size())

		if dstInfo.Size() != srcInfo.Size() {
			return "", fmt.Errorf("文件复制后大小不一致: 源文件=%d, 目标文件=%d", srcInfo.Size(), dstInfo.Size())
		}
	}

	// 9. 存储缩略图（如果生成了）
	var thumbnailPath string
	if needThumbnail && tempThumbnailPath != "" {
		thumbnailPath = filepath.Join(storageDir, virtualFileName+".jpg")
		if err := copyFile(tempThumbnailPath, thumbnailPath); err != nil {
			logger.LOG.Warn("存储缩略图失败", "error", err)
			thumbnailPath = "" // 缩略图失败不影响主流程
		}
		// 临时缩略图会在cleanupTempFiles中一并清理（整个临时目录）
	}

	// 10. 使用数据库事务保证数据一致性
	// 如果是加密文件，加密文件路径就是主文件路径
	var encFilePath string
	if data.IsEnc {
		encFilePath = mainFilePath // 加密文件存储为.data文件
	}

	fileInfo := &models.FileInfo{
		ID:              fileID,
		Name:            data.FileName,
		RandomName:      virtualFileName,
		Size:            int(actualFileSize), // 使用实际计算的文件大小
		Mime:            mimeType,
		ThumbnailImg:    thumbnailPath,
		Path:            mainFilePath,
		FileHash:        fullHash,
		FileEncHash:     fileEncHash, // 加密文件的hash
		ChunkSignature:  data.ChunkSignature,
		FirstChunkHash:  data.FirstChunkHash,
		SecondChunkHash: data.SecondChunkHash,
		ThirdChunkHash:  data.ThirdChunkHash,
		HasFullHash:     true,
		IsEnc:           data.IsEnc,
		IsChunk:         needChunkStorage,
		ChunkCount:      len(chunks),
		EncPath:         encFilePath, // 加密文件的最终存储路径
		CreatedAt:       custom_type.Now(),
		UpdatedAt:       custom_type.Now(),
	}

	userFile := &models.UserFiles{
		UserID:    data.UserID,
		FileID:    fileID,
		IsPublic:  false, // 默认私有
		FileName:  data.FileName,
		CreatedAt: custom_type.Now(),
		UfID:      uuid.NewString(),
	}

	// 开启数据库事务，确保所有数据库操作的原子性
	reportUploadStage(data, "committing")
	err = repoFactory.DB().Transaction(func(tx *gorm.DB) error {
		// 创建基于事务的仓储工厂
		txFactory := repoFactory.WithTx(tx)
		// 目录和文件元数据在同一事务中创建，入库失败时不会遗留空目录。
		if data.DirectoryID > 0 {
			directory, pathErr := txFactory.Directory().GetByID(ctx, data.DirectoryID)
			if pathErr != nil || directory.UserID != data.UserID {
				return fmt.Errorf("虚拟目录不存在或无权访问")
			}
			userFile.DirectoryID = data.DirectoryID
		} else if data.SavePath == "" {
			rootDirectory, pathErr := txFactory.Directory().GetRoot(ctx, data.UserID)
			if pathErr != nil {
				return fmt.Errorf("获取根目录失败: %w", pathErr)
			}
			userFile.DirectoryID = rootDirectory.ID
		} else {
			directoryID, pathErr := virtualpath.EnsureDirectory(ctx, data.UserID, data.SavePath, txFactory)
			if pathErr != nil {
				return fmt.Errorf("获取虚拟目录ID失败: %w", pathErr)
			}
			userFile.DirectoryID = directoryID
		}

		// 10.1 写入文件信息
		if err := txFactory.FileInfo().Create(ctx, fileInfo); err != nil {
			return fmt.Errorf("写入文件信息失败: %w", err)
		}
		if _, err := metadata.Persist(ctx, tx, fileInfo.ID, metadataResult); err != nil {
			return fmt.Errorf("写入文件元数据失败: %w", err)
		}

		// 10.2 写入分片信息（如果是分片存储）
		if len(chunks) > 0 {
			if err := txFactory.FileChunk().BatchCreate(ctx, chunks); err != nil {
				return fmt.Errorf("写入分片信息失败: %w", err)
			}
		}

		// 10.3 写入用户文件关联
		if err := txFactory.UserFiles().Create(ctx, userFile); err != nil {
			return fmt.Errorf("写入用户文件关联失败: %w", err)
		}
		if err := tagging.QueueUserFile(ctx, tx, userFile.UserID, userFile.UfID); err != nil {
			return fmt.Errorf("写入文件标签任务失败: %w", err)
		}

		// 10.4 原子扣减用户剩余空间，避免并发上传或离线下载把空间扣成负数。
		user, err := txFactory.User().GetByID(ctx, data.UserID)
		if err != nil {
			return fmt.Errorf("查询用户信息失败: %w", err)
		}
		if user.Space > 0 { // 如果不是无限空间
			remainingCharge := actualFileSize - data.ReservedSize
			query := tx.Model(&models.UserInfo{}).Where("id = ?", data.UserID)
			if remainingCharge > 0 {
				result := query.Where("free_space >= ?", remainingCharge).
					UpdateColumn("free_space", gorm.Expr("free_space - ?", remainingCharge))
				if result.Error != nil {
					return fmt.Errorf("更新用户剩余空间失败: %w", result.Error)
				}
				if result.RowsAffected != 1 {
					return fmt.Errorf("用户可用空间不足")
				}
			} else if remainingCharge < 0 {
				result := query.UpdateColumn("free_space", gorm.Expr("free_space + ?", -remainingCharge))
				if result.Error != nil {
					return fmt.Errorf("返还用户预留空间失败: %w", result.Error)
				}
			}
		}

		return nil // 事务成功，自动提交
	})

	if err != nil {
		// 事务回滚，需要清理已创建的文件
		cleanupProcessedFiles(mainFilePath, thumbnailPath, chunks)
		return "", err
	}
	repoFactory.NotifyUserFileQueued()

	// 10.5 写入.info文件（保存hash信息）
	if err := writeInfoFile(mainFilePath, fullHash, fileEncHash); err != nil {
		logger.LOG.Warn("写入.info文件失败", "error", err)
		// .info文件写入失败不影响主流程
	}

	logger.LOG.Info("文件处理完成", "fileID", fileID, "fileName", data.FileName, "size", actualFileSize)
	return fileID, nil
}

// mergeChunks 合并分片文件
func mergeChunks(data *FileUploadData) (string, string, error) {
	// 获取临时目录（应该是磁盘temp目录下的文件名子目录）
	tempDir := filepath.Dir(data.TempFilePath)
	mergedPath := filepath.Join(tempDir, "merged_"+filepath.Base(data.FileName))

	mergedFile, err := os.Create(mergedPath)
	if err != nil {
		return "", "", fmt.Errorf("创建合并文件失败: %w", err)
	}
	defer mergedFile.Close()
	hasher := blake3.New()
	writer := io.MultiWriter(mergedFile, hasher)
	buffer := make([]byte, 8*1024*1024)

	// 按索引顺序合并分片
	for i := 0; i < data.ChunkCount; i++ {
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("%d.chunk.data", i))
		chunkFile, err := os.Open(chunkPath)
		if err != nil {
			return "", "", fmt.Errorf("打开分片文件失败 [%d]: %w", i, err)
		}

		if _, err := io.CopyBuffer(writer, chunkFile, buffer); err != nil {
			chunkFile.Close()
			return "", "", fmt.Errorf("合并分片失败 [%d]: %w", i, err)
		}
		chunkFile.Close()
	}

	if err := mergedFile.Sync(); err != nil {
		return "", "", fmt.Errorf("同步合并文件失败: %w", err)
	}
	return mergedPath, hex.EncodeToString(hasher.Sum(nil)), nil
}

// detectMimeType 检测文件MIME类型
func detectMimeType(filePath string) (string, error) {
	// 确保文件句柄立即释放 手动管理文件打开和关闭
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	mime, err := mimetype.DetectReader(file)
	if err != nil {
		return "", fmt.Errorf("检测MIME类型失败: %w", err)
	}
	return mime.String(), nil
}

// isImage 判断MIME类型是否为图片
func isImage(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/")
}

// isVideo 判断MIME类型是否为视频
func isVideo(mimeType string) bool {
	return strings.HasPrefix(mimeType, "video/")
}

// SelectBestDisk 根据存储路径的真实可用空间选择磁盘。
func SelectBestDisk(ctx context.Context, repoFactory *impl.RepositoryFactory, fileSize int64) (*models.Disk, error) {
	if fileSize < 0 {
		return nil, fmt.Errorf("文件大小不能为负数")
	}

	disks, err := repoFactory.Disk().List(ctx, 0, 1000)
	if err != nil {
		return nil, fmt.Errorf("查询磁盘列表失败: %w", err)
	}

	if len(disks) == 0 {
		return nil, fmt.Errorf("没有可用的存储磁盘")
	}

	// 选择真实可用空间最大且能容纳文件的磁盘。
	var bestDisk *models.Disk
	var bestFreeSpace uint64
	var maxAvailableSpace uint64
	var checkedDisks int

	for _, disk := range disks {
		diskInfo, infoErr := util.GetPathDiskSpace(disk.DataPath)
		if infoErr != nil {
			logger.LOG.Warn("获取存储磁盘可用空间失败", "diskID", disk.ID, "dataPath", disk.DataPath, "error", infoErr)
			continue
		}
		checkedDisks++
		if diskInfo.Avail > maxAvailableSpace {
			maxAvailableSpace = diskInfo.Avail
		}
		if diskInfo.Avail >= uint64(fileSize) && (bestDisk == nil || diskInfo.Avail > bestFreeSpace) {
			bestFreeSpace = diskInfo.Avail
			bestDisk = disk
		}
	}

	if checkedDisks == 0 {
		return nil, fmt.Errorf("无法获取存储磁盘的可用空间")
	}
	if bestDisk == nil {
		return nil, fmt.Errorf("没有足够空间的磁盘，需要 %s，最大可用 %s", util.FormatBytes(uint64(fileSize)), util.FormatBytes(maxAvailableSpace))
	}

	return bestDisk, nil
}

// splitAndStoreFile 分片存储大文件
func splitAndStoreFile(filePath, storageDir, virtualFileName, fileID string, chunkSizeGB int) ([]*models.FileChunk, string, error) {
	chunkSize := int64(chunkSizeGB) * 1024 * 1024 * 1024 // GB转字节
	if chunkSize <= 0 {
		return nil, "", fmt.Errorf("大文件存储分片大小必须大于0")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	var chunks []*models.FileChunk
	var chunkIndex uint32 = 0
	buffer := make([]byte, 8*1024*1024)

	for {
		chunkFileName := fmt.Sprintf("%s_%d.data", virtualFileName, chunkIndex)
		chunkPath := filepath.Join(storageDir, chunkFileName)
		chunkFile, err := os.Create(chunkPath)
		if err != nil {
			return nil, "", fmt.Errorf("创建存储分片失败: %w", err)
		}
		hasher := blake3.New()
		n, copyErr := io.CopyBuffer(io.MultiWriter(chunkFile, hasher), io.LimitReader(file, chunkSize), buffer)
		if syncErr := chunkFile.Sync(); syncErr != nil && copyErr == nil {
			copyErr = syncErr
		}
		if closeErr := chunkFile.Close(); closeErr != nil && copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			return nil, "", fmt.Errorf("写入存储分片失败: %w", copyErr)
		}
		if n == 0 {
			_ = os.Remove(chunkPath)
			break
		}

		// 记录分片信息
		chunk := &models.FileChunk{
			ID:         uuid.Must(uuid.NewV7()).String(),
			FileID:     fileID,
			ChunkPath:  chunkPath,
			ChunkSize:  uint64(n),
			ChunkHash:  hex.EncodeToString(hasher.Sum(nil)),
			ChunkIndex: chunkIndex,
		}
		chunks = append(chunks, chunk)
		chunkIndex++
	}

	// 主文件路径返回第一个分片的路径
	mainPath := ""
	if len(chunks) > 0 {
		mainPath = chunks[0].ChunkPath
	}

	return chunks, mainPath, nil
}

func reportUploadStage(data *FileUploadData, stage string) {
	if data.StageCallback != nil {
		data.StageCallback(stage)
	}
}

// copyFile 复制文件
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	return destFile.Sync()
}

// cleanupTempFiles 清理临时文件和临时目录
func cleanupTempFiles(data *FileUploadData) {
	if data.TempFilePath == "" {
		return
	}

	// 获取临时目录（应该是磁盘temp目录下的文件名子目录）
	tempDir := filepath.Dir(data.TempFilePath)

	// Windows系统下文件句柄释放有延迟，需要重试删除整个临时目录
	cleanupDirWithRetry := func(dirPath string, maxRetries int) {
		for i := 0; i < maxRetries; i++ {
			err := os.RemoveAll(dirPath)
			if err == nil || os.IsNotExist(err) {
				logger.LOG.Info("清理临时目录成功", "path", dirPath)
				return
			}
			// 如果是文件被占用错误，等待一下再重试
			if i < maxRetries-1 {
				time.Sleep(time.Millisecond * 200) // 等待200ms
			} else {
				// 最后一次尝试失败，记录警告
				logger.LOG.Warn("清理临时目录失败", "path", dirPath, "error", err, "retries", maxRetries)
			}
		}
	}

	// 清理整个临时目录（包含所有分片、合并文件、加密临时文件等）
	cleanupDirWithRetry(tempDir, 5)
}

// CleanupTaskTempDir 清理上传任务临时目录，并拒绝删除 temp 目录之外的路径。
func CleanupTaskTempDir(tempDir string) error {
	if tempDir == "" {
		return nil
	}
	cleanPath := filepath.Clean(tempDir)
	if filepath.Base(cleanPath) == "." || !strings.EqualFold(filepath.Base(filepath.Dir(cleanPath)), "temp") {
		return fmt.Errorf("拒绝清理非法上传临时目录: %s", tempDir)
	}
	return os.RemoveAll(cleanPath)
}

// FileHashInfo 文件hash信息JSON结构
type FileHashInfo struct {
	FileHash    string `json:"file_hash"`     // 原文件hash
	FileEncHash string `json:"file_enc_hash"` // 加密文件hash
}

// writeInfoFile 写入.info文件（保存hash信息的JSON格式）
func writeInfoFile(dataFilePath, fileHash, fileEncHash string) error {
	// 生成.info文件路径：将.data后缀替换为.info
	infoFilePath := strings.TrimSuffix(dataFilePath, ".data") + ".info"

	// 创建JSON数据
	jsonData := fmt.Sprintf(`{"file_hash":"%s","file_enc_hash":"%s"}`, fileHash, fileEncHash)

	// 写入文件
	if err := os.WriteFile(infoFilePath, []byte(jsonData), 0644); err != nil {
		return fmt.Errorf("写入.info文件失败: %w", err)
	}

	logger.LOG.Debug("写入.info文件成功", "path", infoFilePath)
	return nil
}

// cleanupProcessedFiles 清理已处理的文件（数据库操作失败时回滚）
func cleanupProcessedFiles(mainFilePath, thumbnailPath string, chunks []*models.FileChunk) {
	// 清理主文件
	if mainFilePath != "" {
		if err := os.Remove(mainFilePath); err != nil && !os.IsNotExist(err) {
			logger.LOG.Warn("清理主文件失败", "path", mainFilePath, "error", err)
		}

		// 清理.info文件
		infoPath := strings.TrimSuffix(mainFilePath, ".data") + ".info"
		if err := os.Remove(infoPath); err != nil && !os.IsNotExist(err) {
			logger.LOG.Warn("清理.info文件失败", "path", infoPath, "error", err)
		}
	}

	// 清理缩略图
	if thumbnailPath != "" {
		if err := os.Remove(thumbnailPath); err != nil && !os.IsNotExist(err) {
			logger.LOG.Warn("清理缩略图失败", "path", thumbnailPath, "error", err)
		}
	}

	// 清理分片文件
	for _, chunk := range chunks {
		if chunk.ChunkPath != "" {
			if err := os.Remove(chunk.ChunkPath); err != nil && !os.IsNotExist(err) {
				logger.LOG.Warn("清理分片文件失败", "path", chunk.ChunkPath, "error", err)
			}
		}
	}

	// 清理存储目录（如果为空）
	if mainFilePath != "" {
		storageDir := filepath.Dir(mainFilePath)
		// 尝试删除目录，如果不为空则会失败，这是预期的
		_ = os.Remove(storageDir)
	}

	logger.LOG.Info("已清理处理失败的文件", "mainFilePath", mainFilePath)
}
