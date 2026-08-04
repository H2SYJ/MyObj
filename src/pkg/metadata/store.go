package metadata

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"myobj/src/pkg/models"
)

// Persist 原子保存一次探测结果。相同 provider/key 会被覆盖，版本单调递增。
func Persist(ctx context.Context, db *gorm.DB, fileID string, result Result) (int64, error) {
	var current models.FileMetadataState
	version := int64(1)
	if err := db.WithContext(ctx).Where("file_id = ?", fileID).First(&current).Error; err == nil {
		version = current.Version + 1
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	now := time.Now()
	for _, value := range result.Values {
		if strings.TrimSpace(value.Key) == "" || strings.TrimSpace(value.Provider) == "" {
			continue
		}
		entry := models.FileMetadata{
			ID: uuid.NewString(), FileID: fileID, Provider: value.Provider, Key: value.Key,
			Value: value.Value, ValueType: value.Type, Version: version, UpdatedAt: now,
		}
		if entry.ValueType == "" {
			entry.ValueType = "string"
		}
		if err := db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "file_id"}, {Name: "provider"}, {Name: "key_name"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"value": entry.Value, "value_type": entry.ValueType, "version": version, "updated_at": now,
			}),
		}).Create(&entry).Error; err != nil {
			return 0, err
		}
	}
	status := "ready"
	if result.Partial {
		status = "partial"
	}
	lastError := result.ErrorText()
	if len(lastError) > 2000 {
		lastError = lastError[:2000]
	}
	state := models.FileMetadataState{
		FileID: fileID, Version: version, Status: status, LastError: lastError,
		RetryCount: 0, RunToken: "", UpdatedAt: now,
	}
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "file_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"version": version, "status": status, "last_error": lastError,
			"retry_count": 0, "next_retry_at": nil, "run_token": "", "lease_expires_at": nil, "updated_at": now,
		}),
	}).Create(&state).Error; err != nil {
		return 0, err
	}
	return version, nil
}
