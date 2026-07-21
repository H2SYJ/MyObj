package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/models"
	pluginpkg "myobj/src/pkg/plugin"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PluginService struct {
	factory         *impl.RepositoryFactory
	runtime         *pluginpkg.Runtime
	rootDir         string
	onPluginChanged func(string)
}

func NewPluginService(factory *impl.RepositoryFactory, runtime *pluginpkg.Runtime) *PluginService {
	return &PluginService{factory: factory, runtime: runtime, rootDir: filepath.Join(".", "libs", "plugins")}
}

func (s *PluginService) Runtime() *pluginpkg.Runtime            { return s.runtime }
func (s *PluginService) GetRepository() *impl.RepositoryFactory { return s.factory }
func (s *PluginService) SetChangeHook(hook func(string))        { s.onPluginChanged = hook }
func (s *PluginService) Close(ctx context.Context) error        { return s.runtime.Close(ctx) }

func (s *PluginService) inspectPackage(ctx context.Context, reader io.Reader, size int64) (*pluginpkg.Package, error) {
	pkg, err := pluginpkg.ReadPackage(reader, size)
	if err != nil {
		return nil, err
	}
	if err := s.runtime.ValidateModule(ctx, pkg.WASM); err != nil {
		return nil, err
	}
	response, _, err := s.runtime.Invoke(ctx, pkg.WASMSHA256, pkg.WASM, pluginpkg.InvocationRequest{Action: "healthcheck", Now: time.Now()}, &pluginpkg.InvocationHost{Permissions: map[string]bool{}})
	if err != nil {
		return nil, fmt.Errorf("插件健康检查失败: %w", err)
	}
	if response == nil || !response.OK {
		return nil, fmt.Errorf("插件健康检查失败: 插件未返回成功状态")
	}
	return pkg, nil
}

func (s *PluginService) Inspect(ctx context.Context, reader io.Reader, size int64) (map[string]interface{}, error) {
	pkg, err := s.inspectPackage(ctx, reader, size)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"manifest": pkg.Manifest, "package_sha256": pkg.PackageSHA256, "wasm_sha256": pkg.WASMSHA256,
		"signed": false, "warning": "未签名、管理员信任安装",
	}, nil
}

func (s *PluginService) Install(ctx context.Context, userID string, reader io.Reader, size int64, approvedPermissions []string, trustUnsigned bool) (*models.InstalledPlugin, error) {
	pkg, err := s.inspectPackage(ctx, reader, size)
	if err != nil {
		return nil, err
	}
	if !trustUnsigned {
		return nil, fmt.Errorf("未签名插件必须由管理员明确确认信任")
	}
	sort.Strings(approvedPermissions)
	if strings.Join(approvedPermissions, "\x00") != strings.Join(pkg.Manifest.Permissions, "\x00") {
		return nil, fmt.Errorf("管理员确认的权限与插件声明不一致，请重新审核")
	}
	var existing models.InstalledPlugin
	findErr := s.factory.DB().WithContext(ctx).Where("id = ?", pkg.Manifest.ID).First(&existing).Error
	if findErr == nil && compareVersion(pkg.Manifest.Version, existing.Version) <= 0 {
		return nil, fmt.Errorf("插件版本必须高于已安装版本%s", existing.Version)
	}
	if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, findErr
	}
	manifestJSON, _ := json.Marshal(pkg.Manifest)
	permissionsJSON, _ := json.Marshal(pkg.Manifest.Permissions)
	now := time.Now()
	record := &models.InstalledPlugin{
		ID: pkg.Manifest.ID, Name: pkg.Manifest.Name, Version: pkg.Manifest.Version, APIVersion: pkg.Manifest.APIVersion,
		Author: pkg.Manifest.Author, Description: pkg.Manifest.Description, ManifestJSON: string(manifestJSON),
		PackageSHA256: pkg.PackageSHA256, WASMSHA256: pkg.WASMSHA256,
		Permissions: string(permissionsJSON), Enabled: true, InstalledBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	newPermissions := false
	if findErr == nil {
		record.CreatedAt = existing.CreatedAt
		record.Enabled = existing.Enabled
		if err := s.validateExistingSubscriptions(ctx, record, pkg.WASM); err != nil {
			return nil, err
		}
		newPermissions = hasNewPermissions(existing.Permissions, pkg.Manifest.Permissions)
	}
	packagePath, wasmPath, err := pluginpkg.SavePackage(pkg, s.rootDir)
	if err != nil {
		return nil, fmt.Errorf("保存插件包失败: %w", err)
	}
	if err := verifySavedPlugin(packagePath, wasmPath, pkg.PackageSHA256, pkg.WASMSHA256); err != nil {
		return nil, err
	}
	record.PackagePath, record.WASMPath = packagePath, wasmPath
	if err := s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(record).Error; err != nil {
			return err
		}
		if findErr == nil {
			updates := map[string]interface{}{"plugin_version": record.Version, "updated_at": now}
			if newPermissions {
				updates["status"] = "needs_permission"
				updates["enabled"] = false
			}
			return tx.Model(&models.Subscription{}).Where("plugin_id = ?", record.ID).Updates(updates).Error
		}
		return nil
	}); err != nil {
		_ = os.RemoveAll(filepath.Dir(packagePath))
		return nil, err
	}
	if findErr == nil {
		if newPermissions && s.onPluginChanged != nil {
			s.onPluginChanged(record.ID)
		}
	}
	s.audit(ctx, record.ID, record.Version, userID, "install", "未签名插件由管理员信任安装", "success", "")
	return record, nil
}

func (s *PluginService) validateExistingSubscriptions(ctx context.Context, plugin *models.InstalledPlugin, wasm []byte) error {
	var subscriptions []models.Subscription
	if err := s.factory.DB().WithContext(ctx).Where("plugin_id = ?", plugin.ID).Find(&subscriptions).Error; err != nil {
		return err
	}
	for _, subscription := range subscriptions {
		config, err := decryptSubscriptionConfig(subscription.ID, subscription.UserID, subscription.ConfigEncrypted)
		if err != nil {
			return fmt.Errorf("订阅%s配置无法解密", subscription.ID)
		}
		if _, _, err := s.runtime.Invoke(ctx, plugin.WASMSHA256, wasm, pluginpkg.InvocationRequest{Action: "validate_config", Config: config, Now: time.Now()}, &pluginpkg.InvocationHost{Permissions: map[string]bool{}}); err != nil {
			return fmt.Errorf("新插件版本不兼容订阅%s: %w", subscription.ID, err)
		}
	}
	return nil
}

func (s *PluginService) List(ctx context.Context, enabledOnly bool) ([]map[string]interface{}, error) {
	var records []models.InstalledPlugin
	query := s.factory.DB().WithContext(ctx).Order("name ASC")
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Find(&records).Error; err != nil {
		return nil, err
	}
	result := make([]map[string]interface{}, 0, len(records))
	for _, record := range records {
		var manifest pluginpkg.Manifest
		_ = json.Unmarshal([]byte(record.ManifestJSON), &manifest)
		result = append(result, map[string]interface{}{
			"id": record.ID, "name": record.Name, "version": record.Version, "author": record.Author,
			"description": record.Description, "enabled": record.Enabled, "package_sha256": record.PackageSHA256,
			"wasm_sha256": record.WASMSHA256, "permissions": manifest.Permissions, "config_fields": manifest.ConfigFields,
			"signed": false, "trust_status": "unsigned_admin_trusted",
		})
	}
	return result, nil
}

func (s *PluginService) Toggle(ctx context.Context, id string, enabled bool) error {
	result := s.factory.DB().WithContext(ctx).Model(&models.InstalledPlugin{}).Where("id = ?", id).Updates(map[string]interface{}{"enabled": enabled, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("插件不存在")
	}
	status := "plugin_unavailable"
	if enabled {
		status = "ready"
	}
	if err := s.factory.DB().WithContext(ctx).Model(&models.Subscription{}).Where("plugin_id = ? AND status <> ?", id, "needs_permission").Update("status", status).Error; err != nil {
		return err
	}
	if !enabled && s.onPluginChanged != nil {
		s.onPluginChanged(id)
	}
	s.audit(ctx, id, "", "", "toggle", fmt.Sprintf("enabled=%t", enabled), "success", "")
	return nil
}

func (s *PluginService) Uninstall(ctx context.Context, id string) error {
	var count int64
	if err := s.factory.DB().WithContext(ctx).Model(&models.Subscription{}).Where("plugin_id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("插件仍被%d个订阅使用，不能卸载", count)
	}
	var record models.InstalledPlugin
	if err := s.factory.DB().WithContext(ctx).Where("id = ?", id).First(&record).Error; err != nil {
		return err
	}
	if err := s.factory.DB().WithContext(ctx).Delete(&record).Error; err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(s.rootDir, id)); err != nil {
		return err
	}
	s.audit(ctx, id, record.Version, "", "uninstall", "", "success", "")
	return nil
}

func (s *PluginService) Get(ctx context.Context, id string) (*models.InstalledPlugin, *pluginpkg.Manifest, []byte, error) {
	var record models.InstalledPlugin
	if err := s.factory.DB().WithContext(ctx).Where("id = ?", id).First(&record).Error; err != nil {
		return nil, nil, nil, err
	}
	var manifest pluginpkg.Manifest
	if err := json.Unmarshal([]byte(record.ManifestJSON), &manifest); err != nil {
		return nil, nil, nil, err
	}
	wasm, err := os.ReadFile(record.WASMPath)
	if err != nil {
		return nil, nil, nil, err
	}
	digest := sha256.Sum256(wasm)
	if fmt.Sprintf("%x", digest[:]) != record.WASMSHA256 {
		return nil, nil, nil, fmt.Errorf("插件WASM校验和不匹配")
	}
	return &record, &manifest, wasm, nil
}

func (s *PluginService) Audit(ctx context.Context, page, pageSize int) ([]models.PluginAuditLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	query := s.factory.DB().WithContext(ctx).Model(&models.PluginAuditLog{})
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.PluginAuditLog
	err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	return rows, total, err
}

func (s *PluginService) audit(ctx context.Context, pluginID, version, userID, action, summary, status, errorMsg string) {
	_ = s.factory.DB().WithContext(ctx).Create(&models.PluginAuditLog{
		ID: uuid.NewString(), PluginID: pluginID, PluginVersion: version, UserID: userID,
		Action: action, Summary: summary, Status: status, ErrorMsg: errorMsg, CreatedAt: time.Now(),
	}).Error
}

func verifySavedPlugin(packagePath, wasmPath, packageDigest, wasmDigest string) error {
	for _, item := range []struct {
		path   string
		digest string
		name   string
	}{{packagePath, packageDigest, "插件包"}, {wasmPath, wasmDigest, "WASM"}} {
		content, err := os.ReadFile(item.path)
		if err != nil {
			return fmt.Errorf("复核%s失败: %w", item.name, err)
		}
		digest := sha256.Sum256(content)
		if fmt.Sprintf("%x", digest[:]) != item.digest {
			return fmt.Errorf("%s保存后校验和不匹配", item.name)
		}
	}
	return nil
}

func hasNewPermissions(oldJSON string, current []string) bool {
	var old []string
	_ = json.Unmarshal([]byte(oldJSON), &old)
	set := make(map[string]bool, len(old))
	for _, permission := range old {
		set[permission] = true
	}
	for _, permission := range current {
		if !set[permission] {
			return true
		}
	}
	return false
}

func compareVersion(left, right string) int {
	parse := func(value string) []int {
		value = strings.TrimPrefix(strings.TrimSpace(value), "v")
		value = strings.SplitN(value, "-", 2)[0]
		parts := strings.Split(value, ".")
		result := make([]int, 3)
		for i := 0; i < len(parts) && i < 3; i++ {
			result[i], _ = strconv.Atoi(parts[i])
		}
		return result
	}
	a, b := parse(left), parse(right)
	for i := 0; i < 3; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	prerelease := func(value string) string {
		parts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(value), "v"), "-", 2)
		if len(parts) == 2 {
			return parts[1]
		}
		return ""
	}
	aPre, bPre := prerelease(left), prerelease(right)
	if aPre == "" && bPre != "" {
		return 1
	}
	if aPre != "" && bPre == "" {
		return -1
	}
	return strings.Compare(aPre, bPre)
}
