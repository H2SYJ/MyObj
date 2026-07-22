package impl

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestUserFileOrderUsesFixedColumns(t *testing.T) {
	got := userFileOrder("file_name desc; drop table", "asc")
	if strings.Contains(got, "drop") || got != "user_files.created_at ASC, user_files.uf_id ASC" {
		t.Fatalf("非法排序字段未安全回退: %s", got)
	}
	if got := userFileOrder("size", "desc"); got != "file_info.size DESC, user_files.uf_id ASC" {
		t.Fatalf("大小排序不稳定: %s", got)
	}
}

func TestCountFileReferencesIncludesTrashRecords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE user_files (file_id TEXT NOT NULL, deleted_at DATETIME NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO user_files (file_id, deleted_at) VALUES (?, CURRENT_TIMESTAMP), (?, NULL)", "physical", "physical").Error; err != nil {
		t.Fatal(err)
	}
	count, err := NewRecycledRepository(db).CountFileReferences(context.Background(), "physical")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("引用统计未包含回收站软删除记录: %d", count)
	}
}
