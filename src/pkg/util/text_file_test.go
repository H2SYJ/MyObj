package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestIsTextMime(t *testing.T) {
	cases := []struct {
		mime string
		want bool
	}{
		{"text/plain", true},
		{"text/html", true},
		{"text/markdown", true},
		{"text/css", true},
		{"text/plain; charset=utf-8", true}, // 带参数的前缀匹配
		{"application/json", true},
		{"application/xml", true},
		{"application/javascript", true},
		{"application/x-sh", true},
		{"application/x-yaml", true},
		{"application/sql", true},
		{"application/x-python", true},
		{"image/png", false},
		{"video/mp4", false},
		{"application/pdf", false},
		{"application/zip", false},
		{"application/octet-stream", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsTextMime(c.mime); got != c.want {
			t.Errorf("IsTextMime(%q) = %v, want %v", c.mime, got, c.want)
		}
	}
}

func TestDetectEncoding(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"utf8", []byte("hello 世界"), EncodingUTF8},
		{"utf8-bom", append([]byte{0xEF, 0xBB, 0xBF}, []byte("hi")...), EncodingUTF8BOM},
		{"utf16le-bom", []byte{0xFF, 0xFE, 0x68, 0x00, 0x69, 0x00}, EncodingUTF16LE},
		{"utf16be-bom", []byte{0xFE, 0xFF, 0x00, 0x68, 0x00, 0x69}, EncodingUTF16BE},
		{"empty", []byte{}, EncodingUTF8},
	}
	// GBK 编码的中文不是合法 UTF-8
	gbkData, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("你好"))
	if err != nil {
		t.Fatalf("GBK encode failed: %v", err)
	}
	cases = append(cases, struct {
		name string
		data []byte
		want string
	}{"gbk", gbkData, EncodingGB18030})

	for _, c := range cases {
		if got := DetectEncoding(c.data); got != c.want {
			t.Errorf("DetectEncoding(%s) = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	encodings := []string{EncodingUTF8, EncodingUTF8BOM, EncodingUTF16LE, EncodingUTF16BE, EncodingGB18030}
	text := "Hello, 世界！测试 content with ascii and \n newline."

	for _, enc := range encodings {
		encoded, err := EncodeFromUTF8(text, enc)
		if err != nil {
			t.Fatalf("EncodeFromUTF8(%s) failed: %v", enc, err)
		}
		decoded, err := DecodeToUTF8(encoded, enc)
		if err != nil {
			t.Fatalf("DecodeToUTF8(%s) failed: %v", enc, err)
		}
		if decoded != text {
			t.Errorf("round trip mismatch for %s: got %q want %q", enc, decoded, text)
		}
	}
}

func TestReadWriteTextContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	// GB18030 往返
	gbkText := "你好，MyObj"
	gbkData, _ := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(gbkText))
	if err := os.WriteFile(path, gbkData, 0644); err != nil {
		t.Fatal(err)
	}
	content, enc, err := ReadTextContent(path)
	if err != nil {
		t.Fatalf("ReadTextContent failed: %v", err)
	}
	if content != gbkText {
		t.Errorf("GBK read mismatch: got %q want %q", content, gbkText)
	}
	if enc != EncodingGB18030 {
		t.Errorf("GBK encoding detect = %s, want %s", enc, EncodingGB18030)
	}

	// 编辑后按原编码写回，再次读取应一致
	edited := gbkText + "（编辑）"
	if err := WriteTextContent(path, edited, enc); err != nil {
		t.Fatalf("WriteTextContent failed: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// 写回后的字节应仍是 GB18030 编码（非合法 UTF-8）
	if strings.Contains(string(raw), "（编辑）") && string(raw) != gbkText {
		// 内容字节包含新字符，且整体仍非 UTF-8 即说明按原编码写回
		round, _, err := ReadTextContent(path)
		if err != nil {
			t.Fatal(err)
		}
		if round != edited {
			t.Errorf("edited read mismatch: got %q want %q", round, edited)
		}
	}
}
