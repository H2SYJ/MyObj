package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
)

const APIVersion = "2"

const (
	PermissionPublicHTTP    = "network.public_http"
	PermissionReadMetadata  = "files.read_metadata"
	PermissionCustomHeaders = "downloads.custom_headers"
)

var pluginIDPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{2,127}$`)
var pluginVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)
var configFieldKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

type ConfigField struct {
	Key           string        `json:"key"`
	Label         string        `json:"label"`
	Description   string        `json:"description,omitempty"`
	Type          string        `json:"type"`
	Required      bool          `json:"required,omitempty"`
	Secret        bool          `json:"secret,omitempty"`
	AffectsSource bool          `json:"affects_source,omitempty"`
	Default       interface{}   `json:"default,omitempty"`
	Options       []FieldOption `json:"options,omitempty"`
}

type FieldOption struct {
	Label string      `json:"label"`
	Value interface{} `json:"value"`
}

type Manifest struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Version         string        `json:"version"`
	APIVersion      string        `json:"api_version"`
	Author          string        `json:"author,omitempty"`
	Description     string        `json:"description,omitempty"`
	MinMyObjVersion string        `json:"min_myobj_version,omitempty"`
	Permissions     []string      `json:"permissions,omitempty"`
	ConfigFields    []ConfigField `json:"config_fields,omitempty"`
}

func ParseManifest(data []byte) (*Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(newByteReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("解析manifest.json失败: %w", err)
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return nil, fmt.Errorf("manifest.json包含多余内容")
	}
	if !pluginIDPattern.MatchString(manifest.ID) {
		return nil, fmt.Errorf("插件ID格式无效")
	}
	if manifest.Name == "" || manifest.Version == "" {
		return nil, fmt.Errorf("插件名称和版本不能为空")
	}
	if !pluginVersionPattern.MatchString(manifest.Version) {
		return nil, fmt.Errorf("插件版本必须使用语义化版本格式")
	}
	if manifest.APIVersion != APIVersion {
		return nil, fmt.Errorf("不支持的插件API版本: %s", manifest.APIVersion)
	}
	allowed := map[string]bool{PermissionPublicHTTP: true, PermissionReadMetadata: true, PermissionCustomHeaders: true}
	seen := map[string]bool{}
	for _, permission := range manifest.Permissions {
		if !allowed[permission] {
			return nil, fmt.Errorf("未知插件权限: %s", permission)
		}
		if seen[permission] {
			return nil, fmt.Errorf("插件权限重复: %s", permission)
		}
		seen[permission] = true
	}
	sort.Strings(manifest.Permissions)
	fieldKeys := map[string]bool{}
	for _, field := range manifest.ConfigFields {
		if !configFieldKeyPattern.MatchString(field.Key) || fieldKeys[field.Key] {
			return nil, fmt.Errorf("插件配置字段键为空或重复")
		}
		fieldKeys[field.Key] = true
		switch field.Type {
		case "text", "password", "number", "boolean", "select", "list":
		default:
			return nil, fmt.Errorf("不支持的插件配置字段类型: %s", field.Type)
		}
		if field.Type == "list" && field.Default != nil {
			if _, ok := field.Default.([]interface{}); !ok {
				return nil, fmt.Errorf("list 类型配置字段的默认值必须是字符串数组")
			}
		}
	}
	return &manifest, nil
}

func (m *Manifest) HasPermission(permission string) bool {
	for _, item := range m.Permissions {
		if item == permission {
			return true
		}
	}
	return false
}
