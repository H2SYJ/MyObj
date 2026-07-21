package plugin

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var optionalPluginFiles = []string{"README.md", "icon.png", "icon.jpg", "icon.jpeg", "icon.svg", "icon.webp"}

// BuildPackage 从独立插件项目的产物目录生成可安装的.myobj-plugin包。
func BuildPackage(sourceDir, outputPath string) (*Package, error) {
	files := make(map[string][]byte)
	for _, name := range append([]string{"manifest.json", "plugin.wasm"}, optionalPluginFiles...) {
		path := filepath.Join(sourceDir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) && name != "manifest.json" && name != "plugin.wasm" {
				continue
			}
			return nil, fmt.Errorf("读取%s失败: %w", name, err)
		}
		files[name] = content
	}
	if _, err := ParseManifest(files["manifest.json"]); err != nil {
		return nil, err
	}
	if wasm := files["plugin.wasm"]; len(wasm) == 0 || !bytes.HasPrefix(wasm, []byte{0x00, 0x61, 0x73, 0x6d}) {
		return nil, fmt.Errorf("plugin.wasm不是有效WASM模块")
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var checksums strings.Builder
	for _, name := range names {
		digest := sha256.Sum256(files[name])
		fmt.Fprintf(&checksums, "%x  %s\n", digest, name)
	}
	files["checksums.sha256"] = []byte(checksums.String())
	names = append(names, "checksums.sha256")
	sort.Strings(names)

	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetModTime(time.Unix(0, 0).UTC())
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	if int64(output.Len()) > MaxPackageSize {
		return nil, fmt.Errorf("插件包超过%d字节限制", MaxPackageSize)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return nil, err
	}
	if err := writeFileAtomically(outputPath, output.Bytes(), 0644); err != nil {
		return nil, err
	}
	return ReadPackage(bytes.NewReader(output.Bytes()), int64(output.Len()))
}
