package database

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"myobj/src/pkg/models"
)

func TestMigrateUserFileSearchIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.UserFiles{}); err != nil {
		t.Fatal(err)
	}
	for _, index := range []string{
		"idx_user_files_user_active",
		"idx_user_files_public_active",
		"idx_user_files_user_directory_active",
	} {
		if err := db.Migrator().DropIndex(&models.UserFiles{}, index); err != nil {
			t.Fatal(err)
		}
	}

	if err := migrateUserFileSearchIndexes(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateUserFileSearchIndexes(db); err != nil {
		t.Fatalf("重复迁移应保持幂等: %v", err)
	}
	for _, index := range []string{
		"idx_user_files_user_active",
		"idx_user_files_public_active",
		"idx_user_files_user_directory_active",
	} {
		if !db.Migrator().HasIndex(&models.UserFiles{}, index) {
			t.Fatalf("缺少文件搜索索引: %s", index)
		}
	}
}
