package database

import (
	"os"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
)

// TestMigrateTaggingSchemaMySQL 仅在显式配置可重建的专用测试库时运行。
func TestMigrateTaggingSchemaMySQL(t *testing.T) {
	dsn := os.Getenv("MYOBJ_TEST_MYSQL_DSN")
	if dsn == "" || os.Getenv("MYOBJ_TEST_MYSQL_ALLOW_RESET") != "1" {
		t.Skip("未配置专用MySQL迁移测试库")
	}
	if !strings.Contains(strings.ToLower(dsn), "/myobj_test") {
		t.Fatal("MYOBJ_TEST_MYSQL_DSN必须指向名称以myobj_test开头的专用测试库")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{
		"tag_rebuild_failure", "tag_rebuild_job", "tag_rule", "tag_rule_set",
		"file_metadata_state", "file_metadata", "user_file_tag_state",
		"user_file_tag_exclusion", "user_file_tag", "tag_definition", "tag_category",
		"group_power", "power", "sys_config", "user_files",
	} {
		if err := db.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&models.SysConfig{}, &models.Power{}, &models.GroupPower{}, &models.UserFiles{}); err != nil {
		t.Fatal(err)
	}
	preview := models.Power{ID: 1, Name: "文件预览", Characteristic: "file:preview", CreatedAt: custom_type.Now()}
	if err := db.Create(&preview).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.GroupPower{GroupID: 7, PowerID: preview.ID}).Error; err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		if err := migrateTaggingSchema(db); err != nil {
			t.Fatalf("第%d次MySQL标签迁移失败: %v", run+1, err)
		}
	}
	for model, indexes := range map[interface{}][]string{
		&models.TagDefinition{}:     {"uk_tag_definition"},
		&models.UserFileTag{}:       {"idx_user_tag_file", "idx_uf_source"},
		&models.FileMetadata{}:      {"uk_file_metadata"},
		&models.TagRuleSet{}:        {"idx_tag_rule_scope"},
		&models.TagRebuildJob{}:     {"idx_tag_job_schedule"},
		&models.TagRebuildFailure{}: {"idx_tag_rebuild_failure_status"},
	} {
		for _, index := range indexes {
			if !db.Migrator().HasIndex(model, index) {
				t.Fatalf("MySQL标签迁移缺少索引%s", index)
			}
		}
	}
}
