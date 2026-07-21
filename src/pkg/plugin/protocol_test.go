package plugin

import (
	"encoding/json"
	"testing"
)

func TestDownloadableItemRejectsDuplicateHeaderNames(t *testing.T) {
	var item DownloadableItem
	err := json.Unmarshal([]byte(`{"url":"https://example.com/file","download_type":"http","request_headers":{"Cookie":"a","cookie":"b"}}`), &item)
	if err == nil {
		t.Fatal("大小写不同的重复请求头未被拒绝")
	}
}
