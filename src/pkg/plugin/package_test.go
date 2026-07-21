package plugin

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAndReadPackage(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"id":"org.example.test","name":"测试插件","version":"1.0.0","api_version":"1","permissions":["network.public_http"]}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.wasm"), []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}, 0644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "test.myobj-plugin")
	pkg, err := BuildPackage(dir, output)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.ID != "org.example.test" || len(pkg.PackageSHA256) != 64 || len(pkg.WASMSHA256) != 64 {
		t.Fatalf("打包结果异常: %#v", pkg)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPackage(bytes.NewReader(content), int64(len(content))); err != nil {
		t.Fatal(err)
	}
	// 开发者重复打包同一路径时应原子替换，Windows也不能因目标已存在而失败。
	if _, err := BuildPackage(dir, output); err != nil {
		t.Fatalf("重复打包失败: %v", err)
	}
}

func TestParseManifestRejectsTrailingJSON(t *testing.T) {
	manifest := `{"id":"org.example.test","name":"测试插件","version":"1.0.0","api_version":"1"}`
	if _, err := ParseManifest([]byte(manifest + `{}`)); err == nil {
		t.Fatal("manifest.json包含第二个JSON值时应被拒绝")
	}
}

func TestReadPackageRejectsTraversalDuplicateAndBOM(t *testing.T) {
	tests := []struct {
		name  string
		files []zipTestFile
		want  string
	}{
		{name: "traversal", files: []zipTestFile{{name: "../manifest.json", content: []byte("x")}}, want: "非法路径"},
		{name: "duplicate", files: []zipTestFile{{name: "manifest.json", content: []byte("x")}, {name: "./manifest.json", content: []byte("x")}}, want: "重复文件"},
		{name: "bom", files: []zipTestFile{{name: "manifest.json", content: append([]byte{0xEF, 0xBB, 0xBF}, []byte("{}")...)}, {name: "plugin.wasm", content: []byte{0x00, 0x61, 0x73, 0x6d}}, {name: "checksums.sha256", content: []byte("x")}}, want: "UTF-8无BOM"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := makeTestZIP(t, test.files)
			_, err := ReadPackage(bytes.NewReader(content), int64(len(content)))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("期望错误包含%q，实际为%v", test.want, err)
			}
		})
	}
}

type zipTestFile struct {
	name    string
	content []byte
}

func makeTestZIP(t *testing.T, files []zipTestFile) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, file := range files {
		writer, err := archive.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(file.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
