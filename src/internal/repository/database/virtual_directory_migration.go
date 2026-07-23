package database

import (
	"fmt"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/enum"
	"myobj/src/pkg/models"
	"myobj/src/pkg/virtualpath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const virtualDirectoryMigrationVersion = "20260723_virtual_directory_v2"

type schemaMigration struct {
	Version   string    `gorm:"column:version;type:varchar(128);primaryKey"`
	AppliedAt time.Time `gorm:"column:applied_at;not null"`
}

func (schemaMigration) TableName() string { return "schema_migration" }

type legacyVirtualPath struct {
	ID          int
	UserID      string
	Path        string
	ParentLevel string
	CreatedTime time.Time
	UpdateTime  time.Time
}

func (legacyVirtualPath) TableName() string { return "virtual_path" }

func migrateVirtualDirectorySchema(db *gorm.DB) error {
	var applied int64
	if db.Migrator().HasTable(&schemaMigration{}) {
		if err := db.Model(&schemaMigration{}).Where("version = ?", virtualDirectoryMigrationVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	if applied > 0 {
		return db.AutoMigrate(&models.VirtualDirectory{})
	}

	if !db.Migrator().HasTable("virtual_path") {
		if err := db.AutoMigrate(&models.VirtualDirectory{}); err != nil {
			return err
		}
		if err := validateCurrentVirtualDirectoryData(db); err != nil {
			return err
		}
		if err := markIncompatiblePlugins(db); err != nil {
			return err
		}
		if err := db.AutoMigrate(&schemaMigration{}); err != nil {
			return err
		}
		return db.Create(&schemaMigration{Version: virtualDirectoryMigrationVersion, AppliedAt: time.Now()}).Error
	}

	var legacy []legacyVirtualPath
	if err := db.Table("virtual_path").Order("id ASC").Find(&legacy).Error; err != nil {
		return err
	}
	directories, err := preflightLegacyDirectories(legacy)
	if err != nil {
		return err
	}
	if err := preflightUserDirectoryRoots(db, directories); err != nil {
		return err
	}
	if err := preflightDirectoryReferences(db, directories); err != nil {
		return err
	}
	if err := preflightAbsolutePathColumns(db); err != nil {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&schemaMigration{}); err != nil {
			return err
		}
		if err := tx.AutoMigrate(&models.VirtualDirectory{}); err != nil {
			return err
		}
		// MySQL 的 DDL 不能完整回滚。若上次运行在删除旧表前中断，重新以旧表为准复制。
		if err := tx.Exec("DELETE FROM virtual_directory").Error; err != nil {
			return err
		}
		if len(directories) > 0 {
			if err := tx.Create(&directories).Error; err != nil {
				return fmt.Errorf("迁移虚拟目录失败: %w", err)
			}
		}
		if err := migrateDirectoryReferenceColumn(tx, "user_files", "virtual_path", "directory_id"); err != nil {
			return err
		}
		if err := migrateDirectoryReferenceColumn(tx, "upload_task", "path_id", "directory_id"); err != nil {
			return err
		}
		if err := migrateDirectoryReferenceColumn(tx, "upload_chunk", "path_id", "directory_id"); err != nil {
			return err
		}
		if err := migrateAbsolutePathColumn(tx, "download_task", "virtual_path", "save_path", true); err != nil {
			return err
		}
		if err := migrateAbsolutePathColumn(tx, "subscription", "default_path", "save_path", false); err != nil {
			return err
		}
		if err := validateCopiedVirtualDirectoryData(tx, directories); err != nil {
			return err
		}
		for _, item := range []struct{ table, column string }{
			{"user_files", "virtual_path"},
			{"upload_task", "path_id"},
			{"upload_chunk", "path_id"},
			{"download_task", "virtual_path"},
			{"subscription", "default_path"},
		} {
			if tx.Migrator().HasTable(item.table) && tx.Migrator().HasColumn(item.table, item.column) {
				if err := tx.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", item.table, item.column)).Error; err != nil {
					return err
				}
			}
		}
		if err := enforceMigratedColumnConstraints(tx); err != nil {
			return err
		}
		if err := markIncompatiblePlugins(tx); err != nil {
			return err
		}
		if err := tx.Exec("DROP TABLE virtual_path").Error; err != nil {
			return err
		}
		return tx.Create(&schemaMigration{Version: virtualDirectoryMigrationVersion, AppliedAt: time.Now()}).Error
	})
}

func validateCurrentVirtualDirectoryData(db *gorm.DB) error {
	for _, item := range []struct{ table, oldColumn, newColumn string }{
		{"user_files", "virtual_path", "directory_id"},
		{"upload_task", "path_id", "directory_id"},
		{"upload_chunk", "path_id", "directory_id"},
		{"download_task", "virtual_path", "save_path"},
		{"subscription", "default_path", "save_path"},
	} {
		if !db.Migrator().HasTable(item.table) {
			continue
		}
		if db.Migrator().HasColumn(item.table, item.oldColumn) || !db.Migrator().HasColumn(item.table, item.newColumn) {
			return fmt.Errorf("表%s的目录迁移状态不完整", item.table)
		}
	}
	var current []models.VirtualDirectory
	if err := db.Order("id ASC").Find(&current).Error; err != nil {
		return err
	}
	legacy := make([]legacyVirtualPath, 0, len(current))
	for _, directory := range current {
		parentLevel := ""
		path := directory.Name
		if directory.ParentID == 0 {
			path = "/"
		} else {
			parentLevel = strconv.Itoa(directory.ParentID)
		}
		legacy = append(legacy, legacyVirtualPath{ID: directory.ID, UserID: directory.UserID, Path: path, ParentLevel: parentLevel, CreatedTime: time.Time(directory.CreatedAt), UpdateTime: time.Time(directory.UpdatedAt)})
	}
	validated, err := preflightLegacyDirectories(legacy)
	if err != nil {
		return fmt.Errorf("当前虚拟目录数据无效: %w", err)
	}
	if err := preflightUserDirectoryRoots(db, validated); err != nil {
		return err
	}
	return validateCopiedVirtualDirectoryData(db, validated)
}

func preflightUserDirectoryRoots(db *gorm.DB, directories []models.VirtualDirectory) error {
	if !db.Migrator().HasTable("user_info") {
		return nil
	}
	rootCounts := make(map[string]int)
	for _, directory := range directories {
		if directory.ParentID == 0 {
			rootCounts[directory.UserID]++
		}
	}
	var users []struct{ ID string }
	if err := db.Table("user_info").Select("id").Scan(&users).Error; err != nil {
		return err
	}
	for _, user := range users {
		if rootCounts[user.ID] != 1 {
			return fmt.Errorf("用户%s必须且只能有一个根目录，当前为%d个", user.ID, rootCounts[user.ID])
		}
	}
	return nil
}

func preflightLegacyDirectories(legacy []legacyVirtualPath) ([]models.VirtualDirectory, error) {
	byID := make(map[int]legacyVirtualPath, len(legacy))
	rootCount := map[string]int{}
	directories := make([]models.VirtualDirectory, 0, len(legacy))
	siblings := map[string]int{}
	for _, row := range legacy {
		if row.ID <= 0 {
			return nil, fmt.Errorf("虚拟目录ID无效: %d", row.ID)
		}
		if _, exists := byID[row.ID]; exists {
			return nil, fmt.Errorf("虚拟目录ID重复: %d", row.ID)
		}
		byID[row.ID] = row
		parentID := 0
		if strings.TrimSpace(row.ParentLevel) != "" {
			value, err := strconv.Atoi(row.ParentLevel)
			if err != nil || value <= 0 {
				return nil, fmt.Errorf("虚拟目录%d的父目录ID无效: %q", row.ID, row.ParentLevel)
			}
			parentID = value
		}
		name := ""
		if parentID == 0 {
			rootCount[row.UserID]++
		} else {
			canonical, err := virtualpath.NormalizeDirectoryName(strings.Trim(row.Path, "/"))
			if err != nil {
				return nil, fmt.Errorf("虚拟目录%d名称无效: %w", row.ID, err)
			}
			name = canonical
		}
		key := fmt.Sprintf("%s\x00%d\x00%s", row.UserID, parentID, name)
		if previous, exists := siblings[key]; exists {
			return nil, fmt.Errorf("用户%s的父目录%d存在规范化同名目录%d和%d: %q", row.UserID, parentID, previous, row.ID, name)
		}
		siblings[key] = row.ID
		createdAt, updatedAt := row.CreatedTime, row.UpdateTime
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		if updatedAt.IsZero() {
			updatedAt = createdAt
		}
		directories = append(directories, models.VirtualDirectory{ID: row.ID, UserID: row.UserID, Name: name, ParentID: parentID, CreatedAt: custom_type.JsonTime(createdAt), UpdatedAt: custom_type.JsonTime(updatedAt)})
	}
	for userID, count := range rootCount {
		if count != 1 {
			return nil, fmt.Errorf("用户%s必须且只能有一个根目录，当前为%d个", userID, count)
		}
	}
	for _, directory := range directories {
		if directory.ParentID == 0 {
			continue
		}
		parent, exists := byID[directory.ParentID]
		if !exists || parent.UserID != directory.UserID {
			return nil, fmt.Errorf("虚拟目录%d的父目录不存在或跨用户", directory.ID)
		}
		visited := map[int]struct{}{directory.ID: {}}
		current := directory.ParentID
		for depth := 0; current != 0; depth++ {
			if depth >= virtualpath.MaxDepth {
				return nil, fmt.Errorf("虚拟目录%d超过最大层级", directory.ID)
			}
			if _, exists := visited[current]; exists {
				return nil, fmt.Errorf("虚拟目录%d所在目录树包含循环", directory.ID)
			}
			visited[current] = struct{}{}
			parent := byID[current]
			if strings.TrimSpace(parent.ParentLevel) == "" {
				current = 0
			} else {
				current, _ = strconv.Atoi(parent.ParentLevel)
			}
		}
	}
	convertedByID := make(map[int]models.VirtualDirectory, len(directories))
	for _, directory := range directories {
		convertedByID[directory.ID] = directory
	}
	for _, directory := range directories {
		parts := make([]string, 0)
		current := directory
		for current.ParentID != 0 {
			parts = append([]string{current.Name}, parts...)
			current = convertedByID[current.ParentID]
		}
		if _, err := virtualpath.NormalizeAbsolutePath("/" + strings.Join(parts, "/")); err != nil {
			return nil, fmt.Errorf("虚拟目录%d的完整路径无效: %w", directory.ID, err)
		}
	}
	return directories, nil
}

func preflightAbsolutePathColumns(db *gorm.DB) error {
	if db.Migrator().HasTable("download_task") && db.Migrator().HasColumn("download_task", "virtual_path") {
		var rows []struct {
			ID    string
			Value string
			Type  int
		}
		if err := db.Table("download_task").Select("id, virtual_path AS value, type").Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			value := strings.TrimSpace(row.Value)
			if value == "" {
				if row.Type == enum.DownloadTaskTypeLocalFile.Value() || row.Type == enum.DownloadTaskTypePackage.Value() {
					continue
				}
				return fmt.Errorf("下载任务%s缺少保存目录", row.ID)
			}
			if !strings.HasPrefix(value, "/") {
				value = "/" + value
			}
			if _, err := virtualpath.NormalizeAbsolutePath(value); err != nil {
				return fmt.Errorf("下载任务%s的保存目录无效: %w", row.ID, err)
			}
		}
	}
	if db.Migrator().HasTable("subscription") && db.Migrator().HasColumn("subscription", "default_path") {
		var rows []struct{ ID, Value string }
		if err := db.Table("subscription").Select("id, default_path AS value").Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			value := strings.TrimSpace(row.Value)
			if value == "" {
				return fmt.Errorf("订阅%s缺少保存目录", row.ID)
			}
			if !strings.HasPrefix(value, "/") {
				value = "/" + value
			}
			if _, err := virtualpath.NormalizeAbsolutePath(value); err != nil {
				return fmt.Errorf("订阅%s的保存目录无效: %w", row.ID, err)
			}
		}
	}
	return nil
}

func enforceMigratedColumnConstraints(tx *gorm.DB) error {
	for _, item := range []struct {
		model  interface{}
		field  string
		table  string
		column string
	}{
		{&models.UserFiles{}, "DirectoryID", "user_files", "directory_id"},
		{&models.UploadTask{}, "DirectoryID", "upload_task", "directory_id"},
		{&models.UploadChunk{}, "DirectoryID", "upload_chunk", "directory_id"},
		{&models.Subscription{}, "SavePath", "subscription", "save_path"},
	} {
		if !tx.Migrator().HasTable(item.table) || !tx.Migrator().HasColumn(item.table, item.column) {
			continue
		}
		if err := tx.Migrator().AlterColumn(item.model, item.field); err != nil {
			return fmt.Errorf("约束字段%s.%s失败: %w", item.table, item.column, err)
		}
	}
	return nil
}

func markIncompatiblePlugins(db *gorm.DB) error {
	if db.Migrator().HasTable("installed_plugin") {
		if err := db.Table("installed_plugin").Where("api_version <> ?", "2").Updates(map[string]interface{}{"enabled": false, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
	}
	if db.Migrator().HasTable("subscription") && db.Migrator().HasTable("installed_plugin") {
		return db.Exec("UPDATE subscription SET enabled = ?, status = ?, last_error = ?, next_run_at = NULL, updated_at = ? WHERE plugin_id IN (SELECT id FROM installed_plugin WHERE api_version <> ?)", false, "incompatible_api", "插件API版本不兼容，请升级到v2", time.Now(), "2").Error
	}
	return nil
}

func validateCopiedVirtualDirectoryData(db *gorm.DB, expected []models.VirtualDirectory) error {
	var actual []models.VirtualDirectory
	if err := db.Order("id ASC").Find(&actual).Error; err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("虚拟目录复制校验失败: 期望%d条，实际%d条", len(expected), len(actual))
	}
	expectedByID := make(map[int]models.VirtualDirectory, len(expected))
	owners := make(map[int]string, len(expected))
	for _, directory := range expected {
		expectedByID[directory.ID] = directory
		owners[directory.ID] = directory.UserID
	}
	for _, directory := range actual {
		want, exists := expectedByID[directory.ID]
		if !exists || want.UserID != directory.UserID || want.Name != directory.Name || want.ParentID != directory.ParentID {
			return fmt.Errorf("虚拟目录%d复制校验失败", directory.ID)
		}
	}
	for _, item := range []struct{ table, column string }{{"user_files", "directory_id"}, {"upload_task", "directory_id"}, {"upload_chunk", "directory_id"}} {
		if !db.Migrator().HasTable(item.table) || !db.Migrator().HasColumn(item.table, item.column) {
			continue
		}
		var rows []struct {
			UserID      string
			DirectoryID int
		}
		if err := activeDirectoryReferenceQuery(db, item.table).
			Select("user_id, " + item.column + " AS directory_id").Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if owners[row.DirectoryID] != row.UserID {
				return fmt.Errorf("表%s复制后的目录引用无效: user_id=%s directory_id=%d", item.table, row.UserID, row.DirectoryID)
			}
		}
	}
	if db.Migrator().HasTable("download_task") && db.Migrator().HasColumn("download_task", "save_path") {
		var rows []struct {
			ID       string
			SavePath string
			Type     int
		}
		if err := db.Table("download_task").Select("id, save_path, type").Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if row.SavePath == "" && (row.Type == enum.DownloadTaskTypeLocalFile.Value() || row.Type == enum.DownloadTaskTypePackage.Value()) {
				continue
			}
			normalized, err := virtualpath.NormalizeAbsolutePath(row.SavePath)
			if err != nil || normalized != row.SavePath {
				return fmt.Errorf("下载任务%s复制后的保存目录无效", row.ID)
			}
		}
	}
	if db.Migrator().HasTable("subscription") && db.Migrator().HasColumn("subscription", "save_path") {
		var rows []struct{ ID, SavePath string }
		if err := db.Table("subscription").Select("id, save_path").Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			normalized, err := virtualpath.NormalizeAbsolutePath(row.SavePath)
			if err != nil || normalized != row.SavePath {
				return fmt.Errorf("订阅%s复制后的保存目录无效", row.ID)
			}
		}
	}
	return nil
}

func preflightDirectoryReferences(db *gorm.DB, directories []models.VirtualDirectory) error {
	owners := make(map[int]string, len(directories))
	for _, directory := range directories {
		owners[directory.ID] = directory.UserID
	}
	for _, item := range []struct{ table, column string }{{"user_files", "virtual_path"}, {"upload_task", "path_id"}, {"upload_chunk", "path_id"}} {
		if !db.Migrator().HasTable(item.table) || !db.Migrator().HasColumn(item.table, item.column) {
			continue
		}
		var rows []struct{ UserID, Value string }
		if err := activeDirectoryReferenceQuery(db, item.table).
			Select("user_id, " + item.column + " AS value").Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			id, err := strconv.Atoi(row.Value)
			if err != nil || owners[id] != row.UserID {
				return fmt.Errorf("表%s存在无效目录引用: user_id=%s %s=%q", item.table, row.UserID, item.column, row.Value)
			}
		}
	}
	return nil
}

// activeDirectoryReferenceQuery 只校验当前可见文件的目录引用。
// 整目录进入回收站时，user_files 会被软删除，原目录节点则会被硬删除；
// 恢复目录时会依据回收站映射写入新的目录ID，因此这类历史引用允许暂时悬空。
func activeDirectoryReferenceQuery(db *gorm.DB, table string) *gorm.DB {
	query := db.Table(table)
	if table == "user_files" && db.Migrator().HasColumn(table, "deleted_at") {
		query = query.Where("deleted_at IS NULL")
	}
	return query
}

func migrateDirectoryReferenceColumn(tx *gorm.DB, table, oldColumn, newColumn string) error {
	if !tx.Migrator().HasTable(table) || !tx.Migrator().HasColumn(table, oldColumn) {
		return nil
	}
	if !tx.Migrator().HasColumn(table, newColumn) {
		if err := tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s INTEGER", table, newColumn)).Error; err != nil {
			return err
		}
	}
	if err := tx.Exec(fmt.Sprintf("UPDATE %s SET %s = CAST(%s AS INTEGER)", table, newColumn, oldColumn)).Error; err != nil {
		return err
	}
	return nil
}

func migrateAbsolutePathColumn(tx *gorm.DB, table, oldColumn, newColumn string, allowEmpty bool) error {
	if !tx.Migrator().HasTable(table) || !tx.Migrator().HasColumn(table, oldColumn) {
		return nil
	}
	if !tx.Migrator().HasColumn(table, newColumn) {
		if err := tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s TEXT", table, newColumn)).Error; err != nil {
			return err
		}
	}
	var rows []struct{ ID, Value string }
	if err := tx.Table(table).Select("id, " + oldColumn + " AS value").Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		value := strings.TrimSpace(row.Value)
		if value == "" && allowEmpty {
			continue
		}
		if !strings.HasPrefix(value, "/") {
			value = "/" + value
		}
		normalized, err := virtualpath.NormalizeAbsolutePath(value)
		if err != nil {
			return fmt.Errorf("表%s记录%s的路径无效: %w", table, row.ID, err)
		}
		if err := tx.Table(table).Where("id = ?", row.ID).Update(newColumn, normalized).Error; err != nil {
			return err
		}
	}
	return nil
}
