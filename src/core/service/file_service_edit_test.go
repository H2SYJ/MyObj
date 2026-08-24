package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"golang.org/x/text/encoding/simplifiedchinese"
	"gorm.io/gorm"

	"myobj/src/core/domain/request"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/hash"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/util"
)

// editTestEnv 在线编辑测试环境：内存 SQLite + 真实磁盘目录（{root}/data/... 与 {root}/temp/...）。
type editTestEnv struct {
	db      *gorm.DB
	factory *impl.RepositoryFactory
	svc     *FileService
	root    string
	ctx     context.Context
}

func setupEditTest(t *testing.T, userSpace int64) *editTestEnv {
	t.Helper()
	// 单测环境没有调用 InitLogger，这里兜底初始化一个丢弃输出的 logger，避免日志调用 panic。
	if logger.LOG == nil {
		logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.FileInfo{}, &models.UserFiles{}, &models.UserInfo{}, &models.UserFileTagState{},
	); err != nil {
		t.Fatal(err)
	}
	factory := impl.NewRepositoryFactory(db)
	env := &editTestEnv{db: db, factory: factory, svc: &FileService{factory: factory}, root: t.TempDir(), ctx: context.Background()}
	user := &models.UserInfo{
		ID: "user-1", Name: "tester", UserName: "tester", Password: "x", Email: "e@x.com",
		Phone: "13800000000", GroupID: 1, Space: userSpace, FreeSpace: userSpace, State: 0,
		CreatedAt: custom_type.Now(),
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	return env
}

// createTextFile 创建真实磁盘上的文本文件并写入 FileInfo + UserFiles 关联。
// enc=true 时按 filePassword 加密后落盘（FileInfo.Size 记录加密后大小，与上传语义一致）。
func (e *editTestEnv) createTextFile(t *testing.T, name string, plain []byte, enc bool, filePassword string) (ufID string, fileInfo *models.FileInfo) {
	t.Helper()
	storageDir := filepath.Join(e.root, "data", name)
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(storageDir, "random_name.data")

	stored := plain
	var encHash string
	if enc {
		encKey := util.DeriveEncryptionKey(filePassword, "user-1")
		plainTmp := filepath.Join(e.root, "plain_tmp.bin")
		if err := os.WriteFile(plainTmp, plain, 0644); err != nil {
			t.Fatal(err)
		}
		if err := util.NewFileCrypto(encKey).EncryptFile(plainTmp, path); err != nil {
			t.Fatal(err)
		}
		stored, _ = os.ReadFile(path)
		encHasher := hash.NewFastBlake3Hasher()
		encHashVal, _, err := encHasher.ComputeFileHash(path)
		if err != nil {
			t.Fatal(err)
		}
		encHash = encHashVal
	} else if err := os.WriteFile(path, plain, 0644); err != nil {
		t.Fatal(err)
	}

	fileInfo = &models.FileInfo{
		ID: "file-" + name, Name: name, RandomName: "random_name",
		Size: len(stored), Mime: "text/plain", Path: path,
		FileHash: hash.ComputeBytes(plain), FileEncHash: encHash, HasFullHash: true,
		IsEnc: enc, IsChunk: false, EncPath: path,
		CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now(),
	}
	if err := e.factory.FileInfo().Create(e.ctx, fileInfo); err != nil {
		t.Fatal(err)
	}
	userFile := &models.UserFiles{
		UserID: "user-1", FileID: fileInfo.ID, FileName: name, DirectoryID: 1,
		CreatedAt: custom_type.Now(), UfID: "uf-" + name,
	}
	if err := e.factory.UserFiles().Create(e.ctx, userFile); err != nil {
		t.Fatal(err)
	}
	return userFile.UfID, fileInfo
}

// shareFile 让第二个用户文件引用指向同一物理文件（模拟秒传去重）。
func (e *editTestEnv) shareFile(t *testing.T, fileID, ufID string) {
	t.Helper()
	userFile := &models.UserFiles{
		UserID: "user-1", FileID: fileID, FileName: "dup-" + fileID, DirectoryID: 1,
		CreatedAt: custom_type.Now(), UfID: ufID,
	}
	if err := e.factory.UserFiles().Create(e.ctx, userFile); err != nil {
		t.Fatal(err)
	}
}

func (e *editTestEnv) userInfo(t *testing.T) *models.UserInfo {
	t.Helper()
	var user models.UserInfo
	if err := e.db.Where("id = ?", "user-1").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	return &user
}

func (e *editTestEnv) fileInfoByID(t *testing.T, id string) *models.FileInfo {
	t.Helper()
	fi, err := e.factory.FileInfo().GetByID(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return fi
}

func (e *editTestEnv) tagStateCount(t *testing.T, ufID string) int64 {
	t.Helper()
	var count int64
	if err := e.db.Model(&models.UserFileTagState{}).Where("uf_id = ?", ufID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

// TestEditFileContentInPlaceUTF8 唯一引用的 UTF-8 文件原地覆盖：内容、哈希、大小、配额、标签全部更新。
func TestEditFileContentInPlaceUTF8(t *testing.T) {
	env := setupEditTest(t, 1024*1024)
	ufID, fi := env.createTextFile(t, "notes.txt", []byte("hello world"), false, "")
	_ = fi

	newContent := "hello world edited"
	resp, err := env.svc.EditFileContent(env.ctx, "user-1", &request.EditFileContentRequest{
		FileID: ufID, Content: newContent, BaseHash: hash.ComputeString("hello world"),
	})
	if err != nil {
		t.Fatalf("EditFileContent 失败: %v", err)
	}
	if resp.Encoding != util.EncodingUTF8 {
		t.Errorf("编码应为 utf-8，实际 %s", resp.Encoding)
	}
	if resp.Size != int64(len(newContent)) {
		t.Errorf("大小应为 %d，实际 %d", len(newContent), resp.Size)
	}
	if resp.FileHash != hash.ComputeString(newContent) {
		t.Errorf("哈希不匹配: %s vs %s", resp.FileHash, hash.ComputeString(newContent))
	}

	// 磁盘内容已更新
	diskData, err := os.ReadFile(fi.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(diskData) != newContent {
		t.Errorf("磁盘内容未更新: %q", diskData)
	}

	// FileInfo 已更新（ID 保持不变，原地覆盖）
	updated := env.fileInfoByID(t, fi.ID)
	if updated.Size != len(newContent) || updated.FileHash != hash.ComputeString(newContent) {
		t.Errorf("FileInfo 未正确更新: size=%d hash=%s", updated.Size, updated.FileHash)
	}

	// 配额按 delta 扣减（11 字节 → 18 字节，扣 7）
	delta := int64(len(newContent)) - int64(len("hello world"))
	if got := env.userInfo(t).FreeSpace; got != 1024*1024-delta {
		t.Errorf("free_space 应为 %d，实际 %d", 1024*1024-delta, got)
	}

	// 标签任务已排队
	if env.tagStateCount(t, ufID) != 1 {
		t.Error("标签任务未排队")
	}
}

// TestEditFileContentRedirectWhenShared 多引用（秒传去重）时新建物理文件并重定向，旧文件与其他引用不受影响。
func TestEditFileContentRedirectWhenShared(t *testing.T) {
	env := setupEditTest(t, 0) // 无限空间用户
	uf1, fi := env.createTextFile(t, "shared.txt", []byte("original"), false, "")
	env.shareFile(t, fi.ID, "uf-shared-2")

	newContent := "modified by uf1"
	if _, err := env.svc.EditFileContent(env.ctx, "user-1", &request.EditFileContentRequest{
		FileID: uf1, Content: newContent, BaseHash: hash.ComputeString("original"),
	}); err != nil {
		t.Fatalf("EditFileContent 失败: %v", err)
	}

	// 当前引用重定向到新物理文件
	var uf models.UserFiles
	if err := env.db.Where("uf_id = ?", uf1).First(&uf).Error; err != nil {
		t.Fatal(err)
	}
	if uf.FileID == fi.ID {
		t.Error("uf1 应被重定向到新的物理文件")
	}
	// 其他引用保持指向旧文件
	var uf2 models.UserFiles
	if err := env.db.Where("uf_id = ?", "uf-shared-2").First(&uf2).Error; err != nil {
		t.Fatal(err)
	}
	if uf2.FileID != fi.ID {
		t.Error("uf2 应继续指向旧物理文件")
	}

	// 旧物理文件内容保持不变
	oldData, err := os.ReadFile(fi.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(oldData) != "original" {
		t.Errorf("旧物理文件不应被修改: %q", oldData)
	}
	// 新物理文件内容正确
	newFI := env.fileInfoByID(t, uf.FileID)
	newData, err := os.ReadFile(newFI.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(newData) != newContent {
		t.Errorf("新物理文件内容错误: %q", newData)
	}
	if newFI.ID == fi.ID || newFI.RandomName == fi.RandomName {
		t.Error("新 FileInfo 应具有新 ID 与新存储名")
	}
	// 旧 FileInfo 行仍存在
	if env.fileInfoByID(t, fi.ID) == nil {
		t.Error("旧 FileInfo 不应被删除")
	}
}

// TestEditFileContentBaseHashConflict base_hash 不匹配时应返回冲突错误且不产生任何变更。
func TestEditFileContentBaseHashConflict(t *testing.T) {
	env := setupEditTest(t, 1024*1024)
	ufID, fi := env.createTextFile(t, "conflict.txt", []byte("original"), false, "")

	_, err := env.svc.EditFileContent(env.ctx, "user-1", &request.EditFileContentRequest{
		FileID: ufID, Content: "new", BaseHash: "wrong-hash",
	})
	if !errors.Is(err, ErrFileContentConflict) {
		t.Fatalf("应返回 ErrFileContentConflict，实际 %v", err)
	}

	diskData, _ := os.ReadFile(fi.Path)
	if string(diskData) != "original" {
		t.Errorf("冲突时磁盘不应被修改: %q", diskData)
	}
	updated := env.fileInfoByID(t, fi.ID)
	if updated.Size != len("original") {
		t.Errorf("冲突时 FileInfo 不应被修改: size=%d", updated.Size)
	}
	if got := env.userInfo(t).FreeSpace; got != 1024*1024 {
		t.Errorf("冲突时配额不应变化: %d", got)
	}
}

// TestEditFileContentGB18030RoundTrip GB18030 编码文件编辑后仍以 GB18030 写回，内容正确。
func TestEditFileContentGB18030RoundTrip(t *testing.T) {
	env := setupEditTest(t, 0)
	original := "中文原内容"
	originalBytes, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(original))
	if err != nil {
		t.Fatal(err)
	}
	ufID, fi := env.createTextFile(t, "gb.txt", originalBytes, false, "")

	newContent := "新内容A"
	resp, err := env.svc.EditFileContent(env.ctx, "user-1", &request.EditFileContentRequest{
		FileID: ufID, Content: newContent, BaseHash: hash.ComputeBytes(originalBytes),
	})
	if err != nil {
		t.Fatalf("EditFileContent 失败: %v", err)
	}
	if resp.Encoding != util.EncodingGB18030 {
		t.Errorf("编码应为 gb18030，实际 %s", resp.Encoding)
	}

	diskData, _ := os.ReadFile(fi.Path)
	if string(diskData) == newContent {
		t.Error("磁盘内容不应是 UTF-8 明文")
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(diskData)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != newContent {
		t.Errorf("GB18030 解码后内容错误: %q", decoded)
	}
}

// TestEditFileContentEncrypted 加密文件编辑：需密码、重新加密落盘、解密后为新内容。
func TestEditFileContentEncrypted(t *testing.T) {
	env := setupEditTest(t, 0)
	pwHash, err := util.GeneratePassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.db.Model(&models.UserInfo{}).Where("id = ?", "user-1").Update("file_password", pwHash).Error; err != nil {
		t.Fatal(err)
	}

	originalPlain := []byte("encrypted original")
	ufID, fi := env.createTextFile(t, "enc.txt", originalPlain, true, "secret")

	newContent := "encrypted new content"
	resp, err := env.svc.EditFileContent(env.ctx, "user-1", &request.EditFileContentRequest{
		FileID: ufID, Content: newContent, FilePassword: "secret", BaseHash: hash.ComputeBytes(originalPlain),
	})
	if err != nil {
		t.Fatalf("EditFileContent 失败: %v", err)
	}
	if resp.FileHash != hash.ComputeString(newContent) {
		t.Errorf("明文哈希应为新内容哈希: %s", resp.FileHash)
	}

	// 重新解密验证
	encKey := util.DeriveEncryptionKey("secret", "user-1")
	outPath := filepath.Join(env.root, "decrypted.bin")
	if err := util.NewFileCrypto(encKey).DecryptFile(fi.Path, outPath); err != nil {
		t.Fatal(err)
	}
	decrypted, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != newContent {
		t.Errorf("解密后内容错误: %q", decrypted)
	}

	// FileInfo 加密哈希已更新
	updated := env.fileInfoByID(t, fi.ID)
	encHasher := hash.NewFastBlake3Hasher()
	encHash, _, err := encHasher.ComputeFileHash(fi.Path)
	if err != nil {
		t.Fatal(err)
	}
	if updated.FileEncHash != encHash {
		t.Error("FileEncHash 未更新为新的加密哈希")
	}
}

// TestEditFileContentWrongPassword 加密文件密码错误应拒绝。
func TestEditFileContentWrongPassword(t *testing.T) {
	env := setupEditTest(t, 0)
	pwHash, err := util.GeneratePassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.db.Model(&models.UserInfo{}).Where("id = ?", "user-1").Update("file_password", pwHash).Error; err != nil {
		t.Fatal(err)
	}
	ufID, _ := env.createTextFile(t, "enc2.txt", []byte("data"), true, "secret")

	_, err = env.svc.EditFileContent(env.ctx, "user-1", &request.EditFileContentRequest{
		FileID: ufID, Content: "x", FilePassword: "wrong",
	})
	if err == nil || !strings.Contains(err.Error(), "密码") {
		t.Fatalf("应返回密码错误，实际 %v", err)
	}
}

// TestEditFileContentNotText 非文本类型应拒绝。
func TestEditFileContentNotText(t *testing.T) {
	env := setupEditTest(t, 0)
	storageDir := filepath.Join(env.root, "data", "doc.pdf")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(storageDir, "random.data")
	if err := os.WriteFile(path, []byte("%PDF-1.4"), 0644); err != nil {
		t.Fatal(err)
	}
	fi := &models.FileInfo{
		ID: "file-pdf", Name: "doc.pdf", RandomName: "random", Size: 8, Mime: "application/pdf",
		Path: path, FileHash: hash.ComputeString("%PDF-1.4"), HasFullHash: true,
		CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now(),
	}
	if err := env.factory.FileInfo().Create(env.ctx, fi); err != nil {
		t.Fatal(err)
	}
	userFile := &models.UserFiles{
		UserID: "user-1", FileID: fi.ID, FileName: "doc.pdf", DirectoryID: 1,
		CreatedAt: custom_type.Now(), UfID: "uf-pdf",
	}
	if err := env.factory.UserFiles().Create(env.ctx, userFile); err != nil {
		t.Fatal(err)
	}

	_, err := env.svc.EditFileContent(env.ctx, "user-1", &request.EditFileContentRequest{FileID: "uf-pdf", Content: "x"})
	if err == nil || !strings.Contains(err.Error(), "仅支持文本") {
		t.Fatalf("应拒绝非文本文件，实际 %v", err)
	}
}

// TestEditFileContentSizeLimit 超上限内容应拒绝。
func TestEditFileContentSizeLimit(t *testing.T) {
	env := setupEditTest(t, 0)
	ufID, _ := env.createTextFile(t, "big.txt", []byte("small"), false, "")

	oversized := strings.Repeat("a", util.MaxEditableFileSize+1)
	_, err := env.svc.EditFileContent(env.ctx, "user-1", &request.EditFileContentRequest{FileID: ufID, Content: oversized})
	if err == nil || !strings.Contains(err.Error(), "超过") {
		t.Fatalf("应拒绝超限内容，实际 %v", err)
	}
}

// TestEditFileContentEmptyBaseHash 不传 base_hash 时跳过并发校验，允许保存。
func TestEditFileContentEmptyBaseHash(t *testing.T) {
	env := setupEditTest(t, 0)
	ufID, _ := env.createTextFile(t, "nohash.txt", []byte("old"), false, "")
	resp, err := env.svc.EditFileContent(env.ctx, "user-1", &request.EditFileContentRequest{FileID: ufID, Content: "new"})
	if err != nil {
		t.Fatalf("未传 base_hash 应允许保存: %v", err)
	}
	if resp.Size != int64(len("new")) {
		t.Errorf("大小错误: %d", resp.Size)
	}
}

// TestLoadFileContent 加载接口契约：加密 GB18030 文件返回解码内容、原编码与明文 hash。
func TestLoadFileContent(t *testing.T) {
	env := setupEditTest(t, 0)
	pwHash, err := util.GeneratePassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.db.Model(&models.UserInfo{}).Where("id = ?", "user-1").Update("file_password", pwHash).Error; err != nil {
		t.Fatal(err)
	}

	original := "中文原始内容"
	originalBytes, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(original))
	if err != nil {
		t.Fatal(err)
	}
	ufID, _ := env.createTextFile(t, "gb_enc.txt", originalBytes, true, "secret")

	result, err := env.svc.LoadFileContent(env.ctx, "user-1", ufID, "secret")
	if err != nil {
		t.Fatalf("LoadFileContent 失败: %v", err)
	}
	if result.Content != original {
		t.Errorf("解码内容错误: %q", result.Content)
	}
	if result.Encoding != util.EncodingGB18030 {
		t.Errorf("编码应为 gb18030，实际 %s", result.Encoding)
	}
	if result.FileHash != hash.ComputeBytes(originalBytes) {
		t.Error("FileHash 应为明文原始字节的 blake3")
	}
	if result.Size != int64(len(originalBytes)) {
		t.Errorf("Size 应为明文大小 %d，实际 %d", len(originalBytes), result.Size)
	}

	// 密码错误应拒绝
	if _, err := env.svc.LoadFileContent(env.ctx, "user-1", ufID, "wrong"); err == nil {
		t.Error("密码错误时应拒绝加载")
	}
}

// TestLoadFileContentNotText 非文本文件加载应拒绝。
func TestLoadFileContentNotText(t *testing.T) {
	env := setupEditTest(t, 0)
	storageDir := filepath.Join(env.root, "data", "doc.pdf")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(storageDir, "random.data")
	if err := os.WriteFile(path, []byte("%PDF-1.4"), 0644); err != nil {
		t.Fatal(err)
	}
	fi := &models.FileInfo{
		ID: "file-pdf2", Name: "doc.pdf", RandomName: "random", Size: 8, Mime: "application/pdf",
		Path: path, FileHash: hash.ComputeString("%PDF-1.4"), HasFullHash: true,
		CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now(),
	}
	if err := env.factory.FileInfo().Create(env.ctx, fi); err != nil {
		t.Fatal(err)
	}
	userFile := &models.UserFiles{
		UserID: "user-1", FileID: fi.ID, FileName: "doc.pdf", DirectoryID: 1,
		CreatedAt: custom_type.Now(), UfID: "uf-pdf2",
	}
	if err := env.factory.UserFiles().Create(env.ctx, userFile); err != nil {
		t.Fatal(err)
	}

	if _, err := env.svc.LoadFileContent(env.ctx, "user-1", "uf-pdf2", ""); err == nil || !strings.Contains(err.Error(), "仅支持文本") {
		t.Fatalf("应拒绝非文本文件加载，实际 %v", err)
	}
}
