package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestHTTPMetadataCredentialsRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	_, _, err := GetFileInfoWithClient(context.Background(), server.URL+"/private", server.Client())
	if !IsCredentialsRequired(err) {
		t.Fatalf("HTTP HEAD/GET的401应暂停等待凭据: %v", err)
	}
}

func TestRedactErrorForLogRemovesURLCredentials(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "https://user:secret@example.com/file?token=private#fragment", Err: context.DeadlineExceeded}
	redacted := RedactErrorForLog(err)
	for _, secret := range []string{"secret", "token=private", "fragment"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("错误日志仍包含敏感URL信息%q: %s", secret, redacted)
		}
	}
}

func TestHTTPRangeCredentialsRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	file, openErr := os.CreateTemp(t.TempDir(), "range-*")
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer file.Close()
	chunk := &chunkInfo{Start: 0, End: 0}
	var downloaded int64
	err := downloadChunk(context.Background(), server.URL+"/private", file, chunk, &downloaded, nil, server.Client(), &FileInfoResult{FileSize: 1})
	if !IsCredentialsRequired(err) {
		t.Fatalf("HTTP Range的403应暂停等待凭据: %v", err)
	}
}
