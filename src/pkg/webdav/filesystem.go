package webdav

import (
	"context"
	"fmt"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/repository"
	"myobj/src/pkg/upload"
	"myobj/src/pkg/virtualpath"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/webdav"
)

// MyObjFileSystem WebDAV 文件系统实现
type MyObjFileSystem struct {
	user          *models.UserInfo
	fileRepo      repository.FileInfoRepository
	userFilesRepo repository.UserFilesRepository
	directoryRepo repository.DirectoryRepository
	diskRepo      repository.DiskRepository
	factory       *impl.RepositoryFactory
}

// NewMyObjFileSystem 创建文件系统实例
func NewMyObjFileSystem(user *models.UserInfo, factory *impl.RepositoryFactory) webdav.FileSystem {
	return &MyObjFileSystem{
		user:          user,
		fileRepo:      factory.FileInfo(),
		userFilesRepo: factory.UserFiles(),
		directoryRepo: factory.Directory(),
		diskRepo:      factory.Disk(),
		factory:       factory,
	}
}

// Mkdir 创建目录
func (fs *MyObjFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	logger.LOG.Info("WebDAV Mkdir", "user_id", fs.user.ID, "path", name)

	// 清理路径
	name = fs.cleanPath(name)
	if name == "/" || name == "" {
		return os.ErrExist
	}

	parentPath := path.Dir(name)
	parent, err := fs.resolveDirectory(ctx, parentPath)
	if err != nil {
		return fmt.Errorf("父目录不存在")
	}
	directoryName, err := virtualpath.NormalizeDirectoryName(path.Base(name))
	if err != nil {
		return err
	}
	existing, _ := fs.directoryRepo.GetChild(ctx, fs.user.ID, parent.ID, directoryName)
	if existing != nil {
		return os.ErrExist
	}
	now := custom_type.Now()
	directory := &models.VirtualDirectory{
		UserID: fs.user.ID, Name: directoryName, ParentID: parent.ID, CreatedAt: now, UpdatedAt: now,
	}
	if err := fs.directoryRepo.Create(ctx, directory); err != nil {
		logger.LOG.Error("WebDAV 创建目录失败", "path", name, "error", err)
		return err
	}

	return nil
}

// OpenFile 打开文件
func (fs *MyObjFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	logger.LOG.Info("WebDAV OpenFile", "user_id", fs.user.ID, "path", name, "flag", flag, "isCreate", flag&os.O_CREATE != 0)

	name = fs.cleanPath(name)
	// 如果是系统文件（desktop.ini 等），返回不存在
	if name == "" {
		return nil, os.ErrNotExist
	}

	// 如果是根目录
	if name == "/" {
		return &davDir{
			fs:   fs,
			path: "", // 根目录使用空字符串
			name: "/",
		}, nil
	}

	// 如果是创建模式，直接尝试创建文件（避免锁冲突）
	if flag&os.O_CREATE != 0 {
		// 先检查文件是否已存在
		_, err := fs.getUserFileByPath(ctx, name)
		if err != nil {
			// 文件不存在，创建新文件
			logger.LOG.Info("WebDAV 创建新文件", "path", name)
			return fs.createFile(ctx, name, flag, perm)
		}
	}

	// 目录路径按层级逐段解析，不再直接用完整路径查询节点名称。
	directory, err := fs.resolveDirectory(ctx, name)
	if err == nil {
		logger.LOG.Info("WebDAV OpenFile - 找到目录", "path", name, "directory_id", directory.ID)
		return &davDir{
			fs:   fs,
			path: name, // 使用标准化后的路径（不带前缀 /）
			name: path.Base(name),
		}, nil
	}

	// 尝试作为文件打开
	userFiles, err := fs.getUserFileByPath(ctx, name)
	if err == nil {
		// 获取文件信息
		fileInfo, err := fs.fileRepo.GetByID(ctx, userFiles.FileID)
		if err != nil {
			return nil, err
		}

		// 打开物理文件
		f, err := os.OpenFile(fileInfo.Path, flag, perm)
		if err != nil {
			logger.LOG.Error("WebDAV 打开文件失败", "path", name, "physical_path", fileInfo.Path, "error", err)
			return nil, err
		}

		return &davFile{
			file:      f,
			name:      path.Base(name),
			fileInfo:  fileInfo,
			userFiles: userFiles,
		}, nil
	}

	// 文件/目录不存在，如果是创建模式
	if flag&os.O_CREATE != 0 {
		return fs.createFile(ctx, name, flag, perm)
	}

	return nil, os.ErrNotExist
}

// RemoveAll 删除文件或目录
func (fs *MyObjFileSystem) RemoveAll(ctx context.Context, name string) error {
	logger.LOG.Info("WebDAV RemoveAll", "user_id", fs.user.ID, "path", name)

	name = fs.cleanPath(name)
	if name == "/" || name == "" {
		return os.ErrPermission
	}

	// 尝试删除文件
	userFiles, err := fs.getUserFileByPath(ctx, name)
	if err == nil {
		// 移到回收站
		recycled := &models.Recycled{
			UserID: fs.user.ID,
			FileID: userFiles.FileID,
		}
		recycledRepo := fs.factory.Recycled()
		if err := recycledRepo.Create(ctx, recycled); err != nil {
			logger.LOG.Error("WebDAV 移入回收站失败", "error", err)
			return err
		}

		// 删除 user_files 记录
		if err := fs.userFilesRepo.Delete(ctx, fs.user.ID, userFiles.FileID); err != nil {
			logger.LOG.Error("WebDAV 删除文件记录失败", "error", err)
			return err
		}

		return nil
	}

	// 尝试删除目录
	directory, err := fs.resolveDirectory(ctx, name)
	if err == nil {
		return fs.directoryRepo.Delete(ctx, directory.ID)
	}

	return os.ErrNotExist
}

// Rename 重命名/移动文件或目录
func (fs *MyObjFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	logger.LOG.Info("WebDAV Rename", "user_id", fs.user.ID, "old", oldName, "new", newName)

	oldName = fs.cleanPath(oldName)
	newName = fs.cleanPath(newName)

	// 尝试重命名文件
	userFiles, err := fs.getUserFileByPath(ctx, oldName)
	if err == nil {
		newFileName := path.Base(newName)
		newDir := path.Dir(newName)
		target, err := fs.resolveDirectory(ctx, newDir)
		if err != nil {
			return fmt.Errorf("目标目录不存在")
		}
		files, err := fs.userFilesRepo.ListByDirectoryID(ctx, fs.user.ID, target.ID, 0, 1000)
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.FileName == newFileName && file.UfID != userFiles.UfID {
				return os.ErrExist
			}
		}
		userFiles.FileName = newFileName
		userFiles.DirectoryID = target.ID
		return fs.userFilesRepo.Update(ctx, userFiles)
	}

	// 尝试重命名目录
	directory, err := fs.resolveDirectory(ctx, oldName)
	if err == nil {
		if directory.ParentID == 0 {
			return os.ErrPermission
		}
		targetParent, parentErr := fs.resolveDirectory(ctx, path.Dir(newName))
		if parentErr != nil {
			return fmt.Errorf("目标目录不存在")
		}
		if directory.ID == targetParent.ID || fs.directoryContains(ctx, directory.ID, targetParent.ID) {
			return fmt.Errorf("不能将目录移动到自身或其子目录")
		}
		newDirectoryName, nameErr := virtualpath.NormalizeDirectoryName(path.Base(newName))
		if nameErr != nil {
			return nameErr
		}
		existing, findErr := fs.directoryRepo.GetChild(ctx, fs.user.ID, targetParent.ID, newDirectoryName)
		if findErr == nil && existing.ID != directory.ID {
			return os.ErrExist
		}
		directory.Name = newDirectoryName
		directory.ParentID = targetParent.ID
		directory.UpdatedAt = custom_type.Now()
		return fs.directoryRepo.Update(ctx, directory)
	}

	return os.ErrNotExist
}

func (fs *MyObjFileSystem) directoryContains(ctx context.Context, rootID, targetID int) bool {
	currentID := targetID
	for depth := 0; depth <= virtualpath.MaxDepth && currentID > 0; depth++ {
		if currentID == rootID {
			return true
		}
		current, err := fs.directoryRepo.GetByID(ctx, currentID)
		if err != nil || current.UserID != fs.user.ID {
			return false
		}
		currentID = current.ParentID
	}
	return false
}

// Stat 获取文件/目录信息
func (fs *MyObjFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	name = fs.cleanPath(name)

	// 根目录
	if name == "/" || name == "" {
		return &davFileInfo{
			name:    "/",
			size:    0,
			isDir:   true,
			modTime: time.Now(),
		}, nil
	}

	if _, err := fs.resolveDirectory(ctx, name); err == nil {
		return &davFileInfo{
			name:    path.Base(name),
			size:    0,
			isDir:   true,
			modTime: time.Now(),
		}, nil
	}

	// 尝试查找文件
	userFiles, err := fs.getUserFileByPath(ctx, name)
	if err == nil {
		// 获取文件信息以获取大小
		fileInfo, err := fs.fileRepo.GetByID(ctx, userFiles.FileID)
		if err == nil {
			return &davFileInfo{
				name:    path.Base(name),
				size:    int64(fileInfo.Size),
				isDir:   false,
				modTime: time.Time(userFiles.CreatedAt),
			}, nil
		}
		return &davFileInfo{
			name:    path.Base(name),
			size:    0,
			isDir:   false,
			modTime: time.Time(userFiles.CreatedAt),
		}, nil
	}

	return nil, os.ErrNotExist
}

// cleanPath 清理路径
func (fs *MyObjFileSystem) cleanPath(p string) string {
	p = path.Clean("/" + p)
	if p == "/" {
		return "/"
	}
	// 移除前缀斜杠
	p = strings.TrimPrefix(p, "/")
	// 过滤 Windows 系统文件
	if strings.ToLower(p) == "desktop.ini" || strings.HasSuffix(strings.ToLower(p), "/desktop.ini") {
		return "" // 返回空表示忽略
	}
	return p
}

func (fs *MyObjFileSystem) resolveDirectory(ctx context.Context, raw string) (*models.VirtualDirectory, error) {
	cleaned := fs.cleanPath(raw)
	absolutePath := "/"
	if cleaned != "" && cleaned != "/" && cleaned != "." {
		absolutePath += strings.Trim(cleaned, "/")
	}
	directoryID, err := virtualpath.ResolveDirectoryID(ctx, fs.user.ID, absolutePath, fs.factory)
	if err != nil {
		return nil, err
	}
	return fs.directoryRepo.GetByID(ctx, directoryID)
}

// getUserFileByPath 根据虚拟路径获取用户文件
func (fs *MyObjFileSystem) getUserFileByPath(ctx context.Context, fullPath string) (*models.UserFiles, error) {
	dir := path.Dir(fullPath)
	name := path.Base(fullPath)

	if dir == "." {
		dir = ""
	}

	directory, err := fs.resolveDirectory(ctx, dir)
	if err != nil {
		return nil, os.ErrNotExist
	}
	files, err := fs.userFilesRepo.ListByDirectoryID(ctx, fs.user.ID, directory.ID, 0, 1000)
	if err != nil {
		return nil, err
	}

	// 查找匹配的文件
	for _, f := range files {
		if f.FileName == name {
			return f, nil
		}
	}

	return nil, os.ErrNotExist
}

// createFile 创建新文件
func (fs *MyObjFileSystem) createFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	logger.LOG.Info("WebDAV 创建文件", "user_id", fs.user.ID, "path", name)

	dir := path.Dir(name)
	directory, err := fs.resolveDirectory(ctx, dir)
	if err != nil {
		logger.LOG.Error("WebDAV 目标目录不存在", "dir", dir, "error", err)
		return nil, os.ErrNotExist
	}
	directoryID := directory.ID

	// 2. 选择最大剩余空间的磁盘
	bestDisk, err := fs.diskRepo.GetBigDisk(ctx)
	if err != nil {
		logger.LOG.Error("WebDAV 获取磁盘失败", "error", err)
		return nil, fmt.Errorf("无可用磁盘")
	}

	// 3. 创建临时目录：{DiskPath}/temp/{fileName}_{sessionID}/
	fileName := path.Base(name)
	sessionID := uuid.Must(uuid.NewV7()).String()[:8]
	fileNameWithoutExt := fileName
	if idx := strings.LastIndex(fileName, "."); idx != -1 {
		fileNameWithoutExt = fileName[:idx]
	}
	tempBaseDir := filepath.Join(bestDisk.DataPath, "temp", fmt.Sprintf("%s_%s", fileNameWithoutExt, sessionID))
	if err := os.MkdirAll(tempBaseDir, 0755); err != nil {
		logger.LOG.Error("WebDAV 创建临时目录失败", "error", err, "path", tempBaseDir)
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}

	// 4. 创建临时文件
	tempFilePath := filepath.Join(tempBaseDir, "upload.tmp")
	tempFile, err := os.Create(tempFilePath)
	if err != nil {
		os.RemoveAll(tempBaseDir) // 清理临时目录
		logger.LOG.Error("WebDAV 创建临时文件失败", "error", err, "path", tempFilePath)
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}

	logger.LOG.Info("WebDAV 临时文件已创建", "path", tempFilePath, "diskPath", bestDisk.DataPath)

	// 5. 返回上传文件对象
	return &davUploadFile{
		file:         tempFile,
		name:         fileName,
		tempFilePath: tempFilePath,
		tempDir:      tempBaseDir,
		directoryID:  directoryID,
		userID:       fs.user.ID,
		fs:           fs,
	}, nil
}

// davDir 目录对象
type davDir struct {
	fs   *MyObjFileSystem
	path string
	name string
	pos  int
}

func (d *davDir) Close() error {
	return nil
}

func (d *davDir) Read(p []byte) (int, error) {
	return 0, os.ErrInvalid
}

func (d *davDir) Seek(offset int64, whence int) (int64, error) {
	return 0, os.ErrInvalid
}

func (d *davDir) Readdir(count int) ([]os.FileInfo, error) {
	ctx := context.Background()

	var infos []os.FileInfo

	// 读取子目录
	// d.path 是规范的用户虚拟绝对路径。
	absolutePath := d.path

	logger.LOG.Info("WebDAV Readdir", "user_id", d.fs.user.ID, "absolute_path", absolutePath)

	current, err := d.fs.resolveDirectory(ctx, absolutePath)
	if err != nil {
		logger.LOG.Warn("WebDAV 路径不存在", "absolute_path", absolutePath)
		return infos, nil
	}
	currentDirectoryID := current.ID
	children, _ := d.fs.directoryRepo.ListChildren(ctx, d.fs.user.ID, currentDirectoryID, 0, 1000)
	for _, directory := range children {
		infos = append(infos, &davFileInfo{name: directory.Name, isDir: true, modTime: time.Time(directory.CreatedAt)})
	}
	logger.LOG.Info("WebDAV Readdir - 子文件夹数量", "count", len(infos))

	// 读取文件
	logger.LOG.Info("WebDAV 查询文件", "directory_id", currentDirectoryID)
	files, _ := d.fs.userFilesRepo.ListByDirectoryID(ctx, d.fs.user.ID, currentDirectoryID, 0, 1000)
	for _, f := range files {
		// 获取文件大小
		fileInfo, err := d.fs.fileRepo.GetByID(ctx, f.FileID)
		size := int64(0)
		if err == nil {
			size = int64(fileInfo.Size)
		}
		logger.LOG.Info("WebDAV Readdir - 添加文件", "name", f.FileName, "size", size, "modTime", time.Time(f.CreatedAt))
		infos = append(infos, &davFileInfo{
			name:    f.FileName,
			size:    size,
			isDir:   false,
			modTime: time.Time(f.CreatedAt),
		})
	}

	logger.LOG.Info("WebDAV Readdir - 返回结果", "count", len(infos))
	return infos, nil
}

func (d *davDir) Stat() (os.FileInfo, error) {
	logger.LOG.Info("WebDAV davDir.Stat", "path", d.path, "name", d.name)
	return &davFileInfo{
		name:    d.name,
		isDir:   true,
		size:    0,
		modTime: time.Now(), // 添加修改时间
	}, nil
}

func (d *davDir) Write(p []byte) (int, error) {
	return 0, os.ErrPermission
}

// StatFS 返回文件系统空间信息（用于显示磁盘容量）
func (d *davDir) StatFS() (total, used, avail int64) {
	// 返回用户的存储空间信息
	total = d.fs.user.Space
	used = d.fs.user.Space - d.fs.user.FreeSpace
	avail = d.fs.user.FreeSpace
	return
}

// davFile 文件对象
type davFile struct {
	file      *os.File
	name      string
	fileInfo  *models.FileInfo
	userFiles *models.UserFiles
}

func (f *davFile) Close() error {
	return f.file.Close()
}

func (f *davFile) Read(p []byte) (int, error) {
	return f.file.Read(p)
}

func (f *davFile) Seek(offset int64, whence int) (int64, error) {
	return f.file.Seek(offset, whence)
}

func (f *davFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, os.ErrInvalid
}

func (f *davFile) Stat() (os.FileInfo, error) {
	return &davFileInfo{
		name:    f.name,
		size:    int64(f.fileInfo.Size),
		modTime: time.Time(f.userFiles.CreatedAt),
	}, nil
}

func (f *davFile) Write(p []byte) (int, error) {
	return f.file.Write(p)
}

// davFileInfo 文件信息
type davFileInfo struct {
	name    string
	size    int64
	isDir   bool
	modTime time.Time
}

func (fi *davFileInfo) Name() string { return fi.name }
func (fi *davFileInfo) Size() int64  { return fi.size }
func (fi *davFileInfo) Mode() os.FileMode {
	if fi.isDir {
		return os.ModeDir | 0755
	}
	return 0644
}
func (fi *davFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *davFileInfo) IsDir() bool        { return fi.isDir }
func (fi *davFileInfo) Sys() interface{}   { return nil }

// davUploadFile 上传文件对象
type davUploadFile struct {
	file         *os.File
	name         string
	tempFilePath string
	tempDir      string
	directoryID  int
	userID       string
	fs           *MyObjFileSystem
}

func (f *davUploadFile) Close() error {
	// 1. 关闭文件句柄
	if err := f.file.Close(); err != nil {
		logger.LOG.Error("WebDAV 关闭临时文件失败", "error", err)
		os.RemoveAll(f.tempDir)
		return err
	}

	// 2. 获取文件大小
	fileInfo, err := os.Stat(f.tempFilePath)
	if err != nil {
		logger.LOG.Error("WebDAV 获取文件信息失败", "error", err)
		os.RemoveAll(f.tempDir)
		return err
	}

	fileSize := fileInfo.Size()
	logger.LOG.Info("WebDAV 文件上传完成", "name", f.name, "size", fileSize, "tempPath", f.tempFilePath)

	// 如果文件大小为 0，说明没有数据写入，可能是 LOCK 导致的，直接清理临时文件
	if fileSize == 0 {
		logger.LOG.Warn("WebDAV 文件大小为 0，可能被 LOCK 阻止，不处理此文件", "name", f.name)
		os.RemoveAll(f.tempDir)
		return nil // 返回 nil 避免报错
	}

	logger.LOG.Info("WebDAV 上传到虚拟目录", "directory_id", f.directoryID)

	// 4. 调用上传处理
	uploadData := &upload.FileUploadData{
		TempFilePath: f.tempFilePath,
		FileName:     f.name,
		FileSize:     fileSize,
		DirectoryID:  f.directoryID,
		UserID:       f.userID,
		IsEnc:        false, // WebDAV 不支持加密
		IsChunk:      false, // WebDAV 不支持分片
	}

	fileID, err := upload.ProcessUploadedFile(uploadData, f.fs.factory)
	if err != nil {
		logger.LOG.Error("WebDAV 文件处理失败", "error", err, "name", f.name)
		// 清理临时文件
		os.RemoveAll(f.tempDir)
		return fmt.Errorf("文件上传失败: %w", err)
	}

	// 5. 上传成功，ProcessUploadedFile 会自动清理临时文件
	logger.LOG.Info("WebDAV 文件上传成功", "name", f.name, "fileID", fileID)
	return nil
}

func (f *davUploadFile) Read(p []byte) (int, error) {
	return 0, os.ErrInvalid
}

func (f *davUploadFile) Seek(offset int64, whence int) (int64, error) {
	return f.file.Seek(offset, whence)
}

func (f *davUploadFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, os.ErrInvalid
}

func (f *davUploadFile) Stat() (os.FileInfo, error) {
	return &davFileInfo{
		name:    f.name,
		size:    0,
		isDir:   false,
		modTime: time.Now(),
	}, nil
}

func (f *davUploadFile) Write(p []byte) (int, error) {
	return f.file.Write(p)
}
