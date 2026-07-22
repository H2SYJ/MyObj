package service

import (
	"context"
	"encoding/json"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/models"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSubscriptionListSerializesEmptySecretFieldsAsArray(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Subscription{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	row := models.Subscription{
		ID:                 "subscription-1",
		UserID:             "user-1",
		Name:               "测试订阅",
		PluginID:           "plugin-1",
		PluginVersion:      "1.0.0",
		ConfigEncrypted:    "invalid",
		GrantedPermissions: "[]",
		ScheduleTime:       "08:00",
		DefaultPath:        "/离线下载/订阅",
		InitialLimit:       10,
		MaxItemsPerRun:     100,
		SourceGeneration:   1,
		Enabled:            true,
		Status:             "ready",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}

	service := &SubscriptionService{factory: impl.NewRepositoryFactory(db)}
	views, total, err := service.List(context.Background(), row.UserID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(views) != 1 {
		t.Fatalf("订阅列表数量异常: total=%d len=%d", total, len(views))
	}
	payload, err := json.Marshal(views[0])
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatal(err)
	}
	secretFields, ok := response["secret_fields_configured"].([]interface{})
	if !ok || len(secretFields) != 0 {
		t.Fatalf("空密钥字段必须序列化为数组: %s", payload)
	}
}
