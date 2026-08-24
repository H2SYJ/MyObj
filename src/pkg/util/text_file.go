package util

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

// MaxEditableFileSize 在线编辑允许的最大文件大小（10MB）。
// 超过该大小的文本文件不进入编辑器，避免大文件加载进浏览器内存导致卡顿。
const MaxEditableFileSize = 10 * 1024 * 1024

// 支持的文本编码标识
const (
	EncodingUTF8    = "utf-8"
	EncodingUTF8BOM = "utf-8-bom"
	EncodingUTF16LE = "utf-16le"
	EncodingUTF16BE = "utf-16be"
	EncodingGB18030 = "gb18030" // GBK 超集，兜底覆盖常见中文化文本
)

// textMimePrefixes 文本类 MIME 前缀白名单
var textMimePrefixes = []string{
	"text/",
}

// textMimeExact 文本类 MIME 精确白名单（application 类型中的文本/代码类）
var textMimeExact = map[string]bool{
	"application/json":                  true,
	"application/xml":                   true,
	"application/xhtml+xml":             true,
	"application/x-httpd-php":           true,
	"application/x-sh":                  true,
	"application/x-shellscript":         true,
	"application/x-yaml":                true,
	"application/javascript":            true,
	"application/x-javascript":          true,
	"application/ecmascript":            true,
	"application/x-python-code":         true,
	"application/x-python":              true,
	"application/x-perl":                true,
	"application/x-ruby":                true,
	"application/sql":                   true,
	"application/x-www-form-urlencoded": true,
	"application/vnd.yaml":              true,
	"application/graphql":               true,
}

// IsTextMime 判断 MIME 类型是否属于可在线编辑的文本/代码类型。
// mimetype 库对绝大多数代码文件（.go/.js/.py 等）检测为 text/plain，
// 因此 text/* 前缀已覆盖主要场景，这里再补充常见的 application/* 代码类型。
func IsTextMime(mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if mime == "" {
		return false
	}
	for _, prefix := range textMimePrefixes {
		if strings.HasPrefix(mime, prefix) {
			return true
		}
	}
	return textMimeExact[mime]
}

// DetectEncoding 检测文本字节流的编码。
// 优先级：BOM 明确标记 > 合法 UTF-8 > UTF-16 BOM > GB18030 兜底。
func DetectEncoding(data []byte) string {
	if len(data) >= 3 && bytes.Equal(data[:3], []byte{0xEF, 0xBB, 0xBF}) {
		return EncodingUTF8BOM
	}
	if len(data) >= 2 && bytes.Equal(data[:2], []byte{0xFF, 0xFE}) {
		return EncodingUTF16LE
	}
	if len(data) >= 2 && bytes.Equal(data[:2], []byte{0xFE, 0xFF}) {
		return EncodingUTF16BE
	}
	if utf8.Valid(data) {
		return EncodingUTF8
	}
	// 非 UTF-8：常见中文化文本使用 GBK/GB18030 编码，GB18030 是其超集
	return EncodingGB18030
}

// encoderFor 返回指定编码对应的编码器。
func encoderFor(enc string) encoding.Encoding {
	switch enc {
	case EncodingUTF16LE:
		return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	case EncodingUTF16BE:
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
	case EncodingGB18030:
		return simplifiedchinese.GB18030
	default:
		return nil
	}
}

// DecodeToUTF8 将指定编码的字节流解码为 UTF-8 文本。
func DecodeToUTF8(data []byte, enc string) (string, error) {
	switch enc {
	case EncodingUTF8:
		return string(data), nil
	case EncodingUTF8BOM:
		return string(data[3:]), nil
	}
	e := encoderFor(enc)
	if e == nil {
		return "", fmt.Errorf("不支持的文本编码: %s", enc)
	}
	decoded, err := e.NewDecoder().Bytes(data)
	if err != nil {
		return "", fmt.Errorf("解码文本失败(%s): %w", enc, err)
	}
	return string(decoded), nil
}

// EncodeFromUTF8 将 UTF-8 文本按指定编码编码为字节流，用于保存时还原原编码。
func EncodeFromUTF8(text, enc string) ([]byte, error) {
	switch enc {
	case EncodingUTF8:
		return []byte(text), nil
	case EncodingUTF8BOM:
		out := make([]byte, 0, len(text)+3)
		out = append(out, 0xEF, 0xBB, 0xBF)
		return append(out, []byte(text)...), nil
	}
	e := encoderFor(enc)
	if e == nil {
		return nil, fmt.Errorf("不支持的文本编码: %s", enc)
	}
	encoded, err := e.NewEncoder().Bytes([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("编码文本失败(%s): %w", enc, err)
	}
	return encoded, nil
}

// ReadTextContent 读取文件内容并解码为 UTF-8 文本，同时返回检测到的原编码。
// 调用方需保证文件大小不超过 MaxEditableFileSize。
func ReadTextContent(path string) (string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("读取文件失败: %w", err)
	}
	enc := DetectEncoding(data)
	content, err := DecodeToUTF8(data, enc)
	if err != nil {
		return "", "", err
	}
	return content, enc, nil
}

// WriteTextContent 将 UTF-8 文本按指定编码写入文件（原子写入：先写临时文件再重命名）。
func WriteTextContent(path string, text, enc string) error {
	data, err := EncodeFromUTF8(text, enc)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0644)
}

// atomicWriteFile 将数据原子写入文件：先写同目录临时文件并 fsync，再重命名覆盖目标。
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".edit_*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // 重命名成功后 Remove 会因文件不存在而静默忽略

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("同步临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("设置文件权限失败: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("替换目标文件失败: %w", err)
	}
	return nil
}
