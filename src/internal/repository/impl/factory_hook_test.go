package impl

import (
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestRepositoryFactoryClonesShareUserFileQueueHook(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	factory := NewRepositoryFactory(db)
	clone := factory.Clone()
	txFactory := factory.WithTx(db)
	var notified atomic.Int32
	factory.SetUserFileQueuedHook(func() { notified.Add(1) })

	clone.NotifyUserFileQueued()
	txFactory.NotifyUserFileQueued()
	if notified.Load() != 2 {
		t.Fatalf("克隆或事务工厂没有共享标签任务通知: %d", notified.Load())
	}

	factory.SetUserFileQueuedHook(nil)
	clone.NotifyUserFileQueued()
	if notified.Load() != 2 {
		t.Fatalf("清除通知钩子后仍触发回调: %d", notified.Load())
	}
}
