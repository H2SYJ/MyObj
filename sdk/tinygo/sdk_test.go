package myobjplugin

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestFileQueryMarshalsNameEquals(t *testing.T) {
	encoded, err := json.Marshal(FileQuery{NameEquals: "目标影片.mp4", MaxResponseBytes: minimumFileResponseBytes})
	if err != nil {
		t.Fatalf("编码文件查询失败: %v", err)
	}
	var wire map[string]interface{}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("解析文件查询失败: %v", err)
	}
	if wire["name_equals"] != "目标影片.mp4" {
		t.Fatalf("name_equals = %#v", wire["name_equals"])
	}
	if _, exists := wire["MaxResponseBytes"]; exists {
		t.Fatalf("MaxResponseBytes 不应传给宿主: %#v", wire)
	}
	if _, exists := wire["max_response_bytes"]; exists {
		t.Fatalf("max_response_bytes 不应传给宿主: %#v", wire)
	}
}

func TestFileResponseCapacity(t *testing.T) {
	tests := []struct {
		name      string
		requested int
		want      int
		wantErr   bool
	}{
		{name: "默认限制", want: defaultFileResponseBytes},
		{name: "最小限制", requested: minimumFileResponseBytes, want: minimumFileResponseBytes},
		{name: "最大限制", requested: maximumFileResponseBytes, want: maximumFileResponseBytes},
		{name: "低于最小限制", requested: minimumFileResponseBytes - 1, wantErr: true},
		{name: "超过最大限制", requested: maximumFileResponseBytes + 1, wantErr: true},
		{name: "负数限制", requested: -1, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capacity, err := fileResponseCapacity(test.requested)
			if (err != nil) != test.wantErr {
				t.Fatalf("fileResponseCapacity() error = %v, wantErr = %v", err, test.wantErr)
			}
			if capacity != test.want {
				t.Fatalf("capacity = %d, want %d", capacity, test.want)
			}
		})
	}
}

func TestHTTPResponseCapacity(t *testing.T) {
	tests := []struct {
		name      string
		requested int
		wantLimit int
		wantErr   bool
	}{
		{name: "默认限制", wantLimit: defaultHTTPResponseBytes},
		{name: "最小限制", requested: minimumHTTPResponseBytes, wantLimit: minimumHTTPResponseBytes},
		{name: "最大限制", requested: maximumHTTPResponseBytes, wantLimit: maximumHTTPResponseBytes},
		{name: "低于最小限制", requested: minimumHTTPResponseBytes - 1, wantErr: true},
		{name: "超过最大限制", requested: maximumHTTPResponseBytes + 1, wantErr: true},
		{name: "负数限制", requested: -1, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limit, capacity, err := httpResponseCapacity(test.requested)
			if (err != nil) != test.wantErr {
				t.Fatalf("httpResponseCapacity() error = %v, wantErr = %v", err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if limit != test.wantLimit {
				t.Fatalf("limit = %d, want %d", limit, test.wantLimit)
			}
			wantCapacity := base64.StdEncoding.EncodedLen(test.wantLimit) + maxHTTPResponseMetadataBytes
			if capacity != wantCapacity {
				t.Fatalf("capacity = %d, want %d", capacity, wantCapacity)
			}
		})
	}
}
