package myobjplugin

import (
	"encoding/base64"
	"testing"
)

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
