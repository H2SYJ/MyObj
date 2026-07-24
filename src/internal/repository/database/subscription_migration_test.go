package database

import (
	"myobj/src/pkg/models"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateSubscriptionSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateSubscriptionSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, model := range []interface{}{&models.InstalledPlugin{}, &models.Subscription{}, &models.SubscriptionRun{}, &models.SubscriptionItem{}, &models.PluginAuditLog{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("订阅迁移缺少表: %T", model)
		}
	}
	for _, column := range []string{"request_headers_encrypted", "header_hosts_json", "headers_digest", "thumbnail_status"} {
		if !db.Migrator().HasColumn(&models.SubscriptionItem{}, column) {
			t.Fatalf("subscription_item缺少字段%s", column)
		}
	}
	for _, test := range []struct {
		model interface{}
		index string
	}{
		{model: &models.Subscription{}, index: "idx_subscription_dispatch"},
		{model: &models.SubscriptionItem{}, index: "idx_subscription_thumbnail_dispatch"},
	} {
		if !db.Migrator().HasIndex(test.model, test.index) {
			t.Fatalf("订阅迁移缺少索引%s", test.index)
		}
	}
}
