package service

import (
	"context"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
	pluginpkg "myobj/src/pkg/plugin"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSubscriptionFileGetIsUserIsolated(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.FileInfo{}, &models.VirtualPath{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE user_files (
		user_id TEXT NOT NULL, file_id TEXT NOT NULL, file_name TEXT NOT NULL,
		virtual_path TEXT NOT NULL, public BOOLEAN NOT NULL, created_at DATETIME NOT NULL,
		deleted_at DATETIME NULL, uf_id TEXT NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	now := custom_type.Now()
	for index, userID := range []string{"user-a", "user-b"} {
		root := models.VirtualPath{ID: index + 1, UserID: userID, Path: "/", IsDir: true, CreatedTime: now, UpdateTime: now}
		if err := db.Create(&root).Error; err != nil {
			t.Fatal(err)
		}
		fileID := "file-" + string(rune('a'+index))
		file := models.FileInfo{ID: fileID, Name: fileID, RandomName: fileID, Size: 12, Mime: "text/plain", FileHash: "hash-" + fileID, IsChunk: false, EncPath: "", CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&file).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO user_files(user_id,file_id,file_name,virtual_path,public,created_at,deleted_at,uf_id) VALUES(?,?,?,?,?,?,NULL,?)", userID, fileID, fileID+".txt", root.ID, false, now, "uf-"+userID).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := &SubscriptionService{factory: impl.NewRepositoryFactory(db)}
	response, err := service.queryFilesInternal(context.Background(), "user-a", pluginpkg.FileQueryRequest{Operation: "get", UFID: "uf-user-a"})
	if err != nil || len(response.Files) != 1 || response.Files[0].UFID != "uf-user-a" || response.Files[0].VirtualPath != "/" {
		t.Fatalf("查询自己的文件失败: response=%+v err=%v", response, err)
	}
	_, err = service.queryFilesInternal(context.Background(), "user-a", pluginpkg.FileQueryRequest{Operation: "get", UFID: "uf-user-b"})
	if err == nil || !strings.Contains(err.Error(), "not_found") {
		t.Fatalf("跨用户文件查询必须统一返回not_found: %v", err)
	}
}
