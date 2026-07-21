package request

import (
	"encoding/json"
	"testing"
)

func TestOfflineDownloadRequestRejectsDuplicateHeaders(t *testing.T) {
	for _, payload := range []string{
		`{"url":"https://example.com/file","request_headers":{"Cookie":"a","Cookie":"b"}}`,
		`{"url":"https://example.com/file","request_headers":{"Cookie":"a","cookie":"b"}}`,
	} {
		var request CreateOfflineDownloadRequest
		if err := json.Unmarshal([]byte(payload), &request); err == nil {
			t.Fatalf("重复请求头应被拒绝: %s", payload)
		}
	}
}
