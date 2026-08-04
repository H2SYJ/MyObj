package tagging

import (
	"context"
	"time"

	"myobj/src/pkg/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// QueueUserFile 在业务事务内把用户文件标记为待生成，重复入队会合并为最新任务。
func QueueUserFile(ctx context.Context, db *gorm.DB, userID, ufID string) error {
	now := time.Now()
	state := models.UserFileTagState{
		UFID: ufID, UserID: userID, Status: models.TagStatePending, UpdatedAt: now,
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "uf_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"user_id": userID, "status": models.TagStatePending, "last_error": "",
			"retry_count": 0, "next_retry_at": nil, "run_token": "",
			"lease_expires_at": nil, "updated_at": now,
		}),
	}).Create(&state).Error
}
