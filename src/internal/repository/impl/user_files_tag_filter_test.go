package impl

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"myobj/src/pkg/models"
	"myobj/src/pkg/repository"
)

func TestUserFileTagFiltersRespectModesExclusionsAndPublicVisibility(t *testing.T) {
	db := openUserFileFilterDB(t)
	insertFilterFixture(t, db)
	repo := NewUserFilesRepository(db)
	ctx := context.Background()

	privateManual, err := repo.ListFiltered(ctx, repository.UserFileQuery{PublicOnly: true, TagIDs: []string{"tag-private"}, TagMode: "any", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(privateManual) != 1 || privateManual[0].UfID != "uf-public-manual" {
		t.Fatalf("文件广场泄漏了私有手工标签: %+v", privateManual)
	}

	userVisible, err := repo.ListFiltered(ctx, repository.UserFileQuery{UserID: "user-a", TagIDs: []string{"tag-private"}, TagMode: "any", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(userVisible) != 2 {
		t.Fatalf("个人查询应包含私有手工标签: %+v", userVisible)
	}

	all, err := repo.ListFiltered(ctx, repository.UserFileQuery{UserID: "user-a", TagIDs: []string{"tag-auto", "tag-excluded"}, TagMode: "all", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("all 模式不应把已屏蔽标签视为命中: %+v", all)
	}
	any, err := repo.ListFiltered(ctx, repository.UserFileQuery{UserID: "user-a", TagIDs: []string{"tag-auto", "tag-excluded"}, TagMode: "any", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(any) != 2 {
		t.Fatalf("any 模式应命中任一有效标签: %+v", any)
	}
}

func TestUserFileOtherTypeExcludesKnownTypes(t *testing.T) {
	db := openUserFileFilterDB(t)
	for _, item := range []struct{ ufID, fileID, mime string }{
		{"uf-image", "file-image", "image/png"},
		{"uf-doc", "file-doc", "application/pdf"},
		{"uf-archive", "file-archive", "application/zip"},
		{"uf-other", "file-other", "application/octet-stream"},
	} {
		if err := db.Exec("INSERT INTO file_info(id,size,mime,created_at) VALUES(?,1,?,CURRENT_TIMESTAMP)", item.fileID, item.mime).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO user_files(user_id,file_id,file_name,directory_id,public,created_at,deleted_at,uf_id) VALUES('user-a',?,?,1,0,CURRENT_TIMESTAMP,NULL,?)", item.fileID, item.ufID, item.ufID).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := NewUserFilesRepository(db)
	files, err := repo.ListFiltered(context.Background(), repository.UserFileQuery{UserID: "user-a", FileType: "other", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].UfID != "uf-other" {
		t.Fatalf("other 类型筛选结果错误: %+v", files)
	}
}

func openUserFileFilterDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE file_info (id TEXT PRIMARY KEY, size INTEGER NOT NULL, mime TEXT NOT NULL, created_at DATETIME NOT NULL)`,
		`CREATE TABLE user_files (user_id TEXT NOT NULL, file_id TEXT NOT NULL, file_name TEXT NOT NULL, directory_id INTEGER NOT NULL, public BOOLEAN NOT NULL, created_at DATETIME NOT NULL, deleted_at DATETIME NULL, uf_id TEXT PRIMARY KEY)`,
		`CREATE TABLE tag_definition (id TEXT PRIMARY KEY, name TEXT NOT NULL, normalized_name TEXT NOT NULL, category_id TEXT NOT NULL)`,
		`CREATE TABLE user_file_tag (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, uf_id TEXT NOT NULL, tag_id TEXT NOT NULL, source_type TEXT NOT NULL, visibility TEXT NOT NULL)`,
		`CREATE TABLE user_file_tag_exclusion (user_id TEXT NOT NULL, uf_id TEXT NOT NULL, tag_id TEXT NOT NULL, PRIMARY KEY(user_id,uf_id,tag_id))`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func insertFilterFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, ufID := range []string{"uf-private-manual", "uf-public-manual"} {
		fileID := "file-" + ufID
		if err := db.Exec("INSERT INTO file_info(id,size,mime,created_at) VALUES(?,1,'video/mp4',CURRENT_TIMESTAMP)", fileID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO user_files(user_id,file_id,file_name,directory_id,public,created_at,deleted_at,uf_id) VALUES('user-a',?,?,1,1,CURRENT_TIMESTAMP,NULL,?)", fileID, ufID+".mp4", ufID).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tag := range []string{"tag-private", "tag-auto", "tag-excluded"} {
		if err := db.Exec("INSERT INTO tag_definition(id,name,normalized_name,category_id) VALUES(?,?,?,'other')", tag, tag, tag).Error; err != nil {
			t.Fatal(err)
		}
	}
	bindings := []struct{ id, ufID, tagID, source, visibility string }{
		{"binding-private", "uf-private-manual", "tag-private", models.TagSourceManual, models.TagVisibilityPrivate},
		{"binding-public", "uf-public-manual", "tag-private", models.TagSourceManual, models.TagVisibilityPublic},
		{"binding-auto-1", "uf-private-manual", "tag-auto", models.TagSourceFilename, models.TagVisibilityInherit},
		{"binding-auto-2", "uf-public-manual", "tag-auto", models.TagSourceFilename, models.TagVisibilityInherit},
		{"binding-excluded", "uf-public-manual", "tag-excluded", models.TagSourceRule, models.TagVisibilityInherit},
	}
	for _, binding := range bindings {
		if err := db.Exec("INSERT INTO user_file_tag(id,user_id,uf_id,tag_id,source_type,visibility) VALUES(?,'user-a',?,?,?,?)", binding.id, binding.ufID, binding.tagID, binding.source, binding.visibility).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("INSERT INTO user_file_tag_exclusion(user_id,uf_id,tag_id) VALUES('user-a','uf-public-manual','tag-excluded')").Error; err != nil {
		t.Fatal(err)
	}
}
