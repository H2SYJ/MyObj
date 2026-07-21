package plugin

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	MaxPackageSize    = int64(20 * 1024 * 1024)
	MaxUnpackedSize   = int64(50 * 1024 * 1024)
	MaxManifestSize   = int64(1024 * 1024)
	MaxPluginWASMSize = int64(40 * 1024 * 1024)
)

type Package struct {
	Manifest      *Manifest
	ManifestBytes []byte
	WASM          []byte
	PackageSHA256 string
	WASMSHA256    string
	Raw           []byte
}

func ReadPackage(reader io.Reader, declaredSize int64) (*Package, error) {
	if declaredSize <= 0 || declaredSize > MaxPackageSize {
		return nil, fmt.Errorf("插件包大小必须在1到%d字节之间", MaxPackageSize)
	}
	raw, err := io.ReadAll(io.LimitReader(reader, MaxPackageSize+1))
	if err != nil {
		return nil, fmt.Errorf("读取插件包失败: %w", err)
	}
	if int64(len(raw)) > MaxPackageSize {
		return nil, fmt.Errorf("插件包超过%d字节限制", MaxPackageSize)
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("插件包不是有效ZIP: %w", err)
	}
	files := map[string][]byte{}
	var unpacked int64
	var actualUnpacked int64
	for _, file := range archive.File {
		name := filepath.ToSlash(file.Name)
		clean := filepath.ToSlash(filepath.Clean(name))
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(file.Name) {
			return nil, fmt.Errorf("插件包包含非法路径: %s", file.Name)
		}
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("插件包不能包含符号链接: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if _, exists := files[clean]; exists {
			return nil, fmt.Errorf("插件包包含重复文件: %s", clean)
		}
		unpacked += int64(file.UncompressedSize64)
		if unpacked > MaxUnpackedSize {
			return nil, fmt.Errorf("插件包解压后超过%d字节限制", MaxUnpackedSize)
		}
		stream, openErr := file.Open()
		if openErr != nil {
			return nil, openErr
		}
		content, readErr := io.ReadAll(io.LimitReader(stream, MaxUnpackedSize+1))
		stream.Close()
		if readErr != nil {
			return nil, readErr
		}
		actualUnpacked += int64(len(content))
		if actualUnpacked > MaxUnpackedSize {
			return nil, fmt.Errorf("插件包实际解压后超过%d字节限制", MaxUnpackedSize)
		}
		files[clean] = content
	}
	manifestBytes, ok := files["manifest.json"]
	if !ok || int64(len(manifestBytes)) > MaxManifestSize {
		return nil, fmt.Errorf("插件包缺少有效manifest.json")
	}
	if bytes.HasPrefix(manifestBytes, []byte{0xEF, 0xBB, 0xBF}) || !utf8.Valid(manifestBytes) {
		return nil, fmt.Errorf("manifest.json必须为UTF-8无BOM")
	}
	wasm, ok := files["plugin.wasm"]
	if !ok || len(wasm) == 0 || int64(len(wasm)) > MaxPluginWASMSize || !bytes.HasPrefix(wasm, []byte{0x00, 0x61, 0x73, 0x6d}) {
		return nil, fmt.Errorf("插件包缺少有效plugin.wasm")
	}
	checksums, ok := files["checksums.sha256"]
	if !ok || !utf8.Valid(checksums) {
		return nil, fmt.Errorf("插件包缺少有效checksums.sha256")
	}
	if err := verifyChecksums(files, string(checksums)); err != nil {
		return nil, err
	}
	manifest, err := ParseManifest(manifestBytes)
	if err != nil {
		return nil, err
	}
	packageHash := sha256.Sum256(raw)
	wasmHash := sha256.Sum256(wasm)
	return &Package{
		Manifest: manifest, ManifestBytes: manifestBytes, WASM: wasm, Raw: raw,
		PackageSHA256: hex.EncodeToString(packageHash[:]), WASMSHA256: hex.EncodeToString(wasmHash[:]),
	}, nil
}

func verifyChecksums(files map[string][]byte, checksumText string) error {
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(checksumText, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			return fmt.Errorf("checksums.sha256格式无效")
		}
		name := strings.TrimPrefix(parts[1], "*")
		if name == "checksums.sha256" || seen[name] {
			return fmt.Errorf("checksums.sha256包含无效或重复条目: %s", name)
		}
		content, ok := files[name]
		if !ok {
			return fmt.Errorf("校验和引用不存在的文件: %s", name)
		}
		digest := sha256.Sum256(content)
		if !strings.EqualFold(parts[0], hex.EncodeToString(digest[:])) {
			return fmt.Errorf("文件校验和不匹配: %s", name)
		}
		seen[name] = true
	}
	for _, required := range []string{"manifest.json", "plugin.wasm"} {
		if !seen[required] {
			return fmt.Errorf("checksums.sha256缺少%s", required)
		}
	}
	for name := range files {
		if name != "checksums.sha256" && !seen[name] {
			return fmt.Errorf("checksums.sha256缺少%s", name)
		}
	}
	return nil
}

func SavePackage(pkg *Package, root string) (packagePath, wasmPath string, err error) {
	dir := filepath.Join(root, pkg.Manifest.ID, pkg.Manifest.Version)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}
	packagePath = filepath.Join(dir, pkg.Manifest.ID+"-"+pkg.Manifest.Version+".myobj-plugin")
	wasmPath = filepath.Join(dir, "plugin.wasm")
	if err := writeFileAtomically(packagePath, pkg.Raw, 0644); err != nil {
		return "", "", err
	}
	if err := writeFileAtomically(wasmPath, pkg.WASM, 0644); err != nil {
		return "", "", err
	}
	return packagePath, wasmPath, nil
}

func writeFileAtomically(target string, data []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(target), ".plugin-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err = temp.Write(data); err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(tempName, mode); err != nil {
		return err
	}
	_ = os.Remove(target)
	return os.Rename(tempName, target)
}
