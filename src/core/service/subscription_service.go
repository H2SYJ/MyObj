package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"myobj/src/config"
	"myobj/src/core/domain/request"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/download"
	"myobj/src/pkg/models"
	pluginpkg "myobj/src/pkg/plugin"
	"myobj/src/pkg/virtualpath"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SubscriptionService struct {
	factory         *impl.RepositoryFactory
	pluginService   *PluginService
	downloadService *DownloadService
	stop            chan struct{}
	wake            chan struct{}
	sem             chan struct{}
	activeMu        sync.Mutex
	active          map[string]context.CancelFunc
	pending         map[string]bool
	stopOnce        sync.Once
	location        *time.Location
}

func NewSubscriptionService(factory *impl.RepositoryFactory, plugins *PluginService, downloads *DownloadService) *SubscriptionService {
	service := &SubscriptionService{factory: factory, pluginService: plugins, downloadService: downloads, stop: make(chan struct{}), wake: make(chan struct{}, 1), sem: make(chan struct{}, 2), active: map[string]context.CancelFunc{}, pending: map[string]bool{}, location: subscriptionLocation()}
	plugins.SetChangeHook(service.cancelPluginRuns)
	return service
}

func (s *SubscriptionService) GetRepository() *impl.RepositoryFactory { return s.factory }

func (s *SubscriptionService) AvailablePlugins(ctx context.Context) ([]map[string]interface{}, error) {
	return s.pluginService.List(ctx, true)
}

func (s *SubscriptionService) Start() {
	s.recoverInterruptedRuns()
	s.recoverInterruptedThumbnails()
	go s.loop()
	go s.thumbnailLoop()
	s.Notify()
}

func (s *SubscriptionService) Stop() { s.stopOnce.Do(func() { close(s.stop) }) }

func (s *SubscriptionService) Notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *SubscriptionService) loop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.dispatchDue()
		case <-s.wake:
			s.dispatchDue()
		}
	}
}

func (s *SubscriptionService) dispatchDue() {
	var subscriptions []models.Subscription
	now := time.Now()
	if err := s.factory.DB().Where("enabled = ? AND status = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, "ready", now).Order("next_run_at ASC").Limit(20).Find(&subscriptions).Error; err != nil {
		return
	}
	for i := range subscriptions {
		subscription := subscriptions[i]
		next, err := nextScheduleInLocation(subscription.ScheduleTime, now, s.location)
		if err != nil {
			continue
		}
		result := s.factory.DB().Model(&models.Subscription{}).Where("id = ? AND next_run_at <= ?", subscription.ID, now).Update("next_run_at", next)
		if result.Error == nil && result.RowsAffected == 1 {
			s.queueRun(subscription.ID, "schedule")
		}
	}
}

func (s *SubscriptionService) queueRun(subscriptionID, trigger string) string {
	s.activeMu.Lock()
	if _, running := s.active[subscriptionID]; running || s.pending[subscriptionID] {
		s.activeMu.Unlock()
		return ""
	}
	s.pending[subscriptionID] = true
	s.activeMu.Unlock()
	runID := uuid.Must(uuid.NewV7()).String()
	now := time.Now()
	run := &models.SubscriptionRun{ID: runID, SubscriptionID: subscriptionID, Trigger: trigger, Status: "queued", CreatedAt: now}
	if err := s.factory.DB().Create(run).Error; err != nil {
		s.activeMu.Lock()
		delete(s.pending, subscriptionID)
		s.activeMu.Unlock()
		return ""
	}
	go func() {
		defer func() {
			s.activeMu.Lock()
			delete(s.pending, subscriptionID)
			s.activeMu.Unlock()
		}()
		s.sem <- struct{}{}
		defer func() { <-s.sem }()
		s.executeRun(runID)
	}()
	return runID
}

func (s *SubscriptionService) Create(ctx context.Context, userID string, req *request.SubscriptionCreateRequest) (*models.Subscription, string, error) {
	if err := s.checkUserPermission(ctx, userID); err != nil {
		return nil, "", err
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, "", fmt.Errorf("订阅名称不能为空")
	}
	pluginRecord, manifest, wasm, err := s.pluginService.Get(ctx, req.PluginID)
	if err != nil || !pluginRecord.Enabled {
		return nil, "", fmt.Errorf("插件不存在或未启用")
	}
	permissions, err := validateGrantedPermissions(manifest, req.GrantedPermissions)
	if err != nil {
		return nil, "", err
	}
	if req.InitialLimit == 0 {
		req.InitialLimit = 10
	}
	if req.MaxItemsPerRun == 0 {
		req.MaxItemsPerRun = 100
	}
	if req.InitialLimit < 1 || req.InitialLimit > 100 || req.MaxItemsPerRun < 1 || req.MaxItemsPerRun > 500 {
		return nil, "", fmt.Errorf("首次条数或单次上限超出允许范围")
	}
	defaultPath, err := virtualpath.Normalize(req.DefaultPath)
	if err != nil {
		return nil, "", err
	}
	next, err := nextScheduleInLocation(req.ScheduleTime, time.Now(), s.location)
	if err != nil {
		return nil, "", err
	}
	if _, _, err := s.pluginService.Runtime().Invoke(ctx, pluginRecord.WASMSHA256, wasm, pluginpkg.InvocationRequest{Action: "validate_config", Config: req.Config, Now: time.Now()}, &pluginpkg.InvocationHost{Permissions: map[string]bool{}}); err != nil {
		return nil, "", err
	}
	id := uuid.Must(uuid.NewV7()).String()
	encrypted, err := encryptSubscriptionConfig(id, userID, req.Config)
	if err != nil {
		return nil, "", err
	}
	permissionJSON, _ := json.Marshal(permissions)
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	now := time.Now()
	subscription := &models.Subscription{ID: id, UserID: userID, Name: req.Name, PluginID: req.PluginID, PluginVersion: pluginRecord.Version,
		ConfigEncrypted: encrypted, GrantedPermissions: string(permissionJSON), ScheduleTime: req.ScheduleTime, DefaultPath: defaultPath,
		InitialLimit: req.InitialLimit, MaxItemsPerRun: req.MaxItemsPerRun, SourceGeneration: 1, Enabled: enabled, Status: "ready", NextRunAt: &next, CreatedAt: now, UpdatedAt: now}
	if err := s.factory.DB().WithContext(ctx).Create(subscription).Error; err != nil {
		return nil, "", err
	}
	runNow := true
	if req.RunNow != nil {
		runNow = *req.RunNow
	}
	runID := ""
	if runNow && enabled {
		runID = s.queueRun(subscription.ID, "create")
	}
	return subscription, runID, nil
}

func (s *SubscriptionService) Update(ctx context.Context, userID string, req *request.SubscriptionUpdateRequest) error {
	var subscription models.Subscription
	if err := s.factory.DB().WithContext(ctx).Where("id = ? AND user_id = ?", req.ID, userID).First(&subscription).Error; err != nil {
		return fmt.Errorf("订阅不存在")
	}
	pluginRecord, manifest, wasm, err := s.pluginService.Get(ctx, subscription.PluginID)
	if err != nil {
		return err
	}
	updates := map[string]interface{}{"updated_at": time.Now()}
	permissionsChanged := false
	savePathChanged := false
	if req.Name != nil {
		updates["name"] = strings.TrimSpace(*req.Name)
	}
	if req.DefaultPath != nil {
		value, err := virtualpath.Normalize(*req.DefaultPath)
		if err != nil {
			return err
		}
		updates["default_path"] = value
		savePathChanged = value != subscription.DefaultPath
	}
	if req.ScheduleTime != nil {
		next, err := nextScheduleInLocation(*req.ScheduleTime, time.Now(), s.location)
		if err != nil {
			return err
		}
		updates["schedule_time"] = *req.ScheduleTime
		updates["next_run_at"] = next
	}
	if req.InitialLimit != nil {
		if *req.InitialLimit < 1 || *req.InitialLimit > 100 {
			return fmt.Errorf("首次条数超出范围")
		}
		updates["initial_limit"] = *req.InitialLimit
	}
	if req.MaxItemsPerRun != nil {
		if *req.MaxItemsPerRun < 1 || *req.MaxItemsPerRun > 500 {
			return fmt.Errorf("单次上限超出范围")
		}
		updates["max_items_per_run"] = *req.MaxItemsPerRun
	}
	if req.GrantedPermissions != nil {
		permissions, err := validateGrantedPermissions(manifest, *req.GrantedPermissions)
		if err != nil {
			return err
		}
		encoded, _ := json.Marshal(permissions)
		updates["granted_permissions"] = string(encoded)
		permissionsChanged = string(encoded) != subscription.GrantedPermissions
		if subscription.Status == "needs_permission" {
			if !pluginRecord.Enabled {
				return fmt.Errorf("插件当前不可用")
			}
			updates["status"] = "ready"
			updates["enabled"] = true
		}
	}
	if req.Config != nil {
		mergedConfig := make(map[string]interface{}, len(*req.Config))
		for key, value := range *req.Config {
			mergedConfig[key] = value
		}
		oldConfig, decryptErr := decryptSubscriptionConfig(subscription.ID, userID, subscription.ConfigEncrypted)
		if decryptErr != nil {
			return decryptErr
		}
		for _, field := range manifest.ConfigFields {
			if !field.Secret {
				continue
			}
			value, exists := mergedConfig[field.Key]
			if (!exists || value == nil || fmt.Sprint(value) == "") && oldConfig[field.Key] != nil {
				mergedConfig[field.Key] = oldConfig[field.Key]
			}
		}
		if _, _, err := s.pluginService.Runtime().Invoke(ctx, pluginRecord.WASMSHA256, wasm, pluginpkg.InvocationRequest{Action: "validate_config", Config: mergedConfig, Now: time.Now()}, &pluginpkg.InvocationHost{Permissions: map[string]bool{}}); err != nil {
			return err
		}
		encrypted, err := encryptSubscriptionConfig(subscription.ID, userID, mergedConfig)
		if err != nil {
			return err
		}
		updates["config_encrypted"] = encrypted
		if sourceConfigChanged(manifest, oldConfig, mergedConfig) {
			updates["source_generation"] = gorm.Expr("source_generation + 1")
			updates["last_run_at"] = nil
		}
	}
	if err := s.factory.DB().WithContext(ctx).Model(&models.Subscription{}).Where("id = ? AND user_id = ?", req.ID, userID).Updates(updates).Error; err != nil {
		return err
	}
	if permissionsChanged || savePathChanged {
		// 先持久化安全边界变化，再取消运行，避免执行器继续使用旧授权或旧保存目录。
		s.cancelActive(subscription.ID)
	}
	return nil
}

func (s *SubscriptionService) List(ctx context.Context, userID string, page, pageSize int) ([]subscriptionView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	query := s.factory.DB().WithContext(ctx).Model(&models.Subscription{}).Where("user_id = ?", userID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.Subscription
	err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	views := make([]subscriptionView, 0, len(rows))
	for _, row := range rows {
		view := subscriptionView{Subscription: row, GrantedPermissions: decodePermissions(row.GrantedPermissions)}
		configValue, decryptErr := decryptSubscriptionConfig(row.ID, row.UserID, row.ConfigEncrypted)
		if decryptErr == nil {
			_, manifest, _, pluginErr := s.pluginService.Get(ctx, row.PluginID)
			if pluginErr == nil {
				for _, field := range manifest.ConfigFields {
					if field.Secret {
						if value, exists := configValue[field.Key]; exists && value != nil && fmt.Sprint(value) != "" {
							view.SecretFieldsConfigured = append(view.SecretFieldsConfigured, field.Key)
						}
						delete(configValue, field.Key)
					}
				}
			}
			view.Config = configValue
		}
		views = append(views, view)
	}
	return views, total, nil
}

type subscriptionView struct {
	models.Subscription
	Config                 map[string]interface{} `json:"config,omitempty"`
	GrantedPermissions     []string               `json:"granted_permissions"`
	SecretFieldsConfigured []string               `json:"secret_fields_configured"`
}

func (s *SubscriptionService) Toggle(ctx context.Context, userID, id string, enabled bool) error {
	updates := map[string]interface{}{"enabled": enabled, "updated_at": time.Now()}
	if enabled {
		var subscription models.Subscription
		if err := s.factory.DB().Where("id = ? AND user_id = ?", id, userID).First(&subscription).Error; err != nil {
			return err
		}
		next, err := nextScheduleInLocation(subscription.ScheduleTime, time.Now(), s.location)
		if err != nil {
			return err
		}
		if subscription.Status == "needs_permission" {
			return fmt.Errorf("插件权限需要重新确认")
		}
		pluginRecord, _, _, pluginErr := s.pluginService.Get(ctx, subscription.PluginID)
		if pluginErr != nil || !pluginRecord.Enabled {
			return fmt.Errorf("插件当前不可用")
		}
		updates["status"] = "ready"
		updates["next_run_at"] = next
	}
	result := s.factory.DB().WithContext(ctx).Model(&models.Subscription{}).Where("id = ? AND user_id = ?", id, userID).Updates(updates)
	if result.RowsAffected == 0 {
		return fmt.Errorf("订阅不存在")
	}
	if !enabled {
		s.cancelActive(id)
	}
	return result.Error
}

func (s *SubscriptionService) Delete(ctx context.Context, userID, id string) error {
	s.cancelActive(id)
	return s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.Subscription{}).Where("id = ? AND user_id = ?", id, userID).Count(&count).Error; err != nil || count != 1 {
			return fmt.Errorf("订阅不存在")
		}
		if err := tx.Where("subscription_id = ?", id).Delete(&models.SubscriptionRun{}).Error; err != nil {
			return err
		}
		if err := tx.Where("subscription_id = ?", id).Delete(&models.SubscriptionItem{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND user_id = ?", id, userID).Delete(&models.Subscription{}).Error
	})
}

func (s *SubscriptionService) RunNow(ctx context.Context, userID, id string) (string, error) {
	var subscription models.Subscription
	if err := s.factory.DB().WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&subscription).Error; err != nil {
		return "", fmt.Errorf("订阅不存在")
	}
	if !subscription.Enabled || subscription.Status != "ready" {
		return "", fmt.Errorf("订阅未启用或需要处理权限/插件状态")
	}
	runID := s.queueRun(id, "manual")
	if runID == "" {
		return "", fmt.Errorf("订阅正在运行或等待运行")
	}
	return runID, nil
}

func (s *SubscriptionService) History(ctx context.Context, userID, subscriptionID, kind string, page, pageSize int) (interface{}, int64, error) {
	var count int64
	if err := s.factory.DB().WithContext(ctx).Model(&models.Subscription{}).Where("id = ? AND user_id = ?", subscriptionID, userID).Count(&count).Error; err != nil || count != 1 {
		return nil, 0, fmt.Errorf("订阅不存在")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	if kind == "runs" {
		var total int64
		var rows []models.SubscriptionRun
		query := s.factory.DB().WithContext(ctx).Model(&models.SubscriptionRun{}).Where("subscription_id = ?", subscriptionID)
		query.Count(&total)
		err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
		return rows, total, err
	}
	var total int64
	var rows []models.SubscriptionItem
	query := s.factory.DB().WithContext(ctx).Model(&models.SubscriptionItem{}).Where("subscription_id = ?", subscriptionID)
	query.Count(&total)
	err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	views := make([]subscriptionItemView, 0, len(rows))
	for _, row := range rows {
		view := subscriptionItemView{SubscriptionItem: row, HasRequestHeaders: row.RequestHeadersEncrypted != ""}
		if row.RequestHeadersEncrypted != "" {
			if headers, decryptErr := download.DecryptRequestHeaders(config.CONFIG.Auth.Secret, row.ID, userID, row.RequestHeadersEncrypted); decryptErr == nil {
				for name := range headers {
					view.RequestHeaderNames = append(view.RequestHeaderNames, name)
				}
				sort.Strings(view.RequestHeaderNames)
			}
		}
		if row.DownloadTaskID != "" {
			var task models.DownloadTask
			if taskErr := s.factory.DB().Select("state", "requires_headers", "error_msg").Where("id = ? AND user_id = ?", row.DownloadTaskID, userID).First(&task).Error; taskErr == nil {
				view.DownloadState = &task.State
				view.RequiresHeaders = task.RequiresHeaders
				view.DownloadError = task.ErrorMsg
			}
		}
		views = append(views, view)
	}
	return views, total, nil
}

type subscriptionItemView struct {
	models.SubscriptionItem
	HasRequestHeaders  bool     `json:"has_request_headers"`
	RequestHeaderNames []string `json:"request_header_names"`
	DownloadState      *int     `json:"download_state,omitempty"`
	RequiresHeaders    bool     `json:"requires_headers"`
	DownloadError      string   `json:"download_error,omitempty"`
}

func (s *SubscriptionService) executeRun(runID string) {
	ctx := context.Background()
	var run models.SubscriptionRun
	if err := s.factory.DB().Where("id = ? AND status = ?", runID, "queued").First(&run).Error; err != nil {
		return
	}
	var subscription models.Subscription
	if err := s.factory.DB().Where("id = ?", run.SubscriptionID).First(&subscription).Error; err != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.activeMu.Lock()
	if _, exists := s.active[subscription.ID]; exists {
		s.activeMu.Unlock()
		s.failRun(&run, fmt.Errorf("订阅正在运行"))
		return
	}
	s.active[subscription.ID] = cancel
	s.activeMu.Unlock()
	defer func() { cancel(); s.activeMu.Lock(); delete(s.active, subscription.ID); s.activeMu.Unlock() }()
	if err := s.checkUserPermission(runCtx, subscription.UserID); err != nil {
		s.failRun(&run, err)
		return
	}
	pluginRecord, manifest, wasm, err := s.pluginService.Get(runCtx, subscription.PluginID)
	if err != nil || !pluginRecord.Enabled {
		s.factory.DB().Model(&models.Subscription{}).Where("id = ?", subscription.ID).Updates(map[string]interface{}{"status": "plugin_unavailable", "enabled": false, "updated_at": time.Now()})
		s.failRun(&run, fmt.Errorf("插件不可用"))
		return
	}
	permissions := decodePermissions(subscription.GrantedPermissions)
	for _, permission := range permissions {
		if !manifest.HasPermission(permission) {
			s.factory.DB().Model(&models.Subscription{}).Where("id = ?", subscription.ID).Updates(map[string]interface{}{"status": "needs_permission", "enabled": false, "updated_at": time.Now()})
			s.failRun(&run, fmt.Errorf("订阅权限需要重新确认"))
			return
		}
	}
	now := time.Now()
	lease := now.Add(2 * time.Minute)
	token := uuid.NewString()
	claimed := s.factory.DB().Model(&models.SubscriptionRun{}).Where("id = ? AND status = ?", run.ID, "queued").Updates(map[string]interface{}{"status": "running", "run_token": token, "started_at": now, "lease_expires_at": lease})
	if claimed.Error != nil || claimed.RowsAffected != 1 {
		return
	}
	run.RunToken = token
	heartbeatDone := make(chan struct{})
	go s.heartbeatRun(runCtx, run.ID, token, heartbeatDone)
	defer close(heartbeatDone)
	configValue, err := decryptSubscriptionConfig(subscription.ID, subscription.UserID, subscription.ConfigEncrypted)
	if err != nil {
		s.failRun(&run, err)
		return
	}
	httpClient, err := download.NewPublicHTTPClient(s.downloadService.networkPolicy.ProxyURL(), s.downloadService.networkPolicy.DownloadLimiter())
	if err != nil {
		s.failRun(&run, err)
		return
	}
	host := &pluginpkg.InvocationHost{Permissions: permissionMap(permissions), HTTPClient: httpClient, ValidateHTTPURL: download.ValidatePublicHTTPURL, FileQuery: func(callCtx context.Context, query pluginpkg.FileQueryRequest) (pluginpkg.FileQueryResponse, error) {
		return s.queryFiles(callCtx, subscription, *pluginRecord, query)
	}}
	started := time.Now()
	response, _, err := s.pluginService.Runtime().Invoke(runCtx, pluginRecord.WASMSHA256, wasm, pluginpkg.InvocationRequest{Action: "fetch", Config: configValue, Now: time.Now()}, host)
	if err != nil {
		s.writeAudit(subscription, *pluginRecord, "fetch", 0, time.Since(started), "failed", err.Error())
		s.failRun(&run, err)
		return
	}
	s.writeAudit(subscription, *pluginRecord, "fetch", len(response.Items), time.Since(started), "success", "")
	var latest models.Subscription
	if err := s.factory.DB().Where("id = ? AND enabled = ? AND status = ?", subscription.ID, true, "ready").First(&latest).Error; err != nil {
		s.failRun(&run, fmt.Errorf("permission_denied"))
		return
	}
	if strings.Join(decodePermissions(latest.GrantedPermissions), "\x00") != strings.Join(permissions, "\x00") {
		s.failRun(&run, fmt.Errorf("permission_denied"))
		return
	}
	subscription = latest
	found, created, skipped, processErr := s.processItems(runCtx, &subscription, manifest, response.Items)
	status := "success"
	errorMsg := ""
	if processErr != nil {
		status = "partial"
		errorMsg = processErr.Error()
	}
	finished := time.Now()
	s.factory.DB().Model(&models.SubscriptionRun{}).Where("id = ? AND run_token = ?", run.ID, token).Updates(map[string]interface{}{"status": status, "items_found": found, "tasks_created": created, "items_skipped": skipped, "error_msg": errorMsg, "finished_at": finished, "lease_expires_at": nil, "run_token": ""})
	s.factory.DB().Model(&models.Subscription{}).Where("id = ?", subscription.ID).Updates(map[string]interface{}{"last_run_at": finished, "last_error": errorMsg, "updated_at": finished})
}

func (s *SubscriptionService) processItems(ctx context.Context, subscription *models.Subscription, manifest *pluginpkg.Manifest, items []pluginpkg.DownloadableItem) (int, int, int, error) {
	found := len(items)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].PublishedAt == nil {
			return false
		}
		if items[j].PublishedAt == nil {
			return true
		}
		return items[i].PublishedAt.After(*items[j].PublishedAt)
	})
	if len(items) > 500 {
		items = items[:500]
	}
	initial := subscription.LastRunAt == nil
	created, skipped, submittedBefore := 0, 0, 0
	if initial {
		var submittedCount int64
		_ = s.factory.DB().Model(&models.SubscriptionItem{}).
			Where("subscription_id = ? AND source_generation = ? AND download_task_id <> ''", subscription.ID, subscription.SourceGeneration).
			Count(&submittedCount).Error
		submittedBefore = int(submittedCount)
	}
	errorsFound := []string{}
	permissions := permissionMap(decodePermissions(subscription.GrantedPermissions))
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return found, created, skipped, fmt.Errorf("permission_denied")
		}
		key := itemKey(item)
		var existing models.SubscriptionItem
		err := s.factory.DB().Where("subscription_id = ? AND source_generation = ? AND item_key = ?", subscription.ID, subscription.SourceGeneration, key).First(&existing).Error
		if err == nil {
			submittedExisting := false
			if err := s.refreshExistingItem(ctx, subscription, manifest, &existing, item, permissions); err != nil {
				errorsFound = append(errorsFound, err.Error())
				skipped++
				continue
			}
			if (existing.Status == "deferred" || existing.Status == "submit_failed") && s.canSubmitItem(initial, submittedBefore+created, created, subscription) {
				if reloadErr := s.factory.DB().Where("id = ?", existing.ID).First(&existing).Error; reloadErr == nil {
					_, submitErr := s.downloadService.EnqueueSubscriptionDownload(ctx, subscriptionDownloadInput(subscription.UserID, &existing))
					if submitErr != nil {
						s.factory.DB().Model(&existing).Updates(map[string]interface{}{"status": "submit_failed", "error_msg": submitErr.Error(), "updated_at": time.Now()})
						errorsFound = append(errorsFound, submitErr.Error())
					} else {
						created++
						submittedExisting = true
					}
				} else {
					errorsFound = append(errorsFound, reloadErr.Error())
				}
			}
			if !submittedExisting {
				skipped++
			}
			continue
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			errorsFound = append(errorsFound, err.Error())
			continue
		}
		path, err := virtualpath.JoinSubscriptionPath(subscription.DefaultPath, item.SavePath)
		if err != nil {
			errorsFound = append(errorsFound, err.Error())
			continue
		}
		if item.DownloadType != "http" && item.DownloadType != "hls" {
			errorsFound = append(errorsFound, "插件返回了不支持的下载类型")
			continue
		}
		if err := download.ValidatePublicHTTPURL(item.URL); err != nil {
			errorsFound = append(errorsFound, err.Error())
			continue
		}
		if item.ThumbnailURL != "" {
			if err := download.ValidatePublicHTTPURL(item.ThumbnailURL); err != nil {
				errorsFound = append(errorsFound, "缩略图地址无效")
				continue
			}
		}
		if len(item.RequestHeaders) > 0 && (!manifest.HasPermission(pluginpkg.PermissionCustomHeaders) || !permissions[pluginpkg.PermissionCustomHeaders]) {
			errorsFound = append(errorsFound, "插件未获自定义下载头权限")
			continue
		}
		headers, hosts, err := download.NormalizeRequestConfig(item.URL, item.RequestHeaders, item.HeaderHosts)
		if err != nil {
			errorsFound = append(errorsFound, err.Error())
			continue
		}
		if len(headers) == 0 && len(item.HeaderHosts) > 0 {
			errorsFound = append(errorsFound, "未配置请求头时不能单独配置请求头主机")
			continue
		}
		itemID := uuid.Must(uuid.NewV7()).String()
		encrypted, err := download.EncryptRequestHeaders(config.CONFIG.Auth.Secret, itemID, subscription.UserID, headers)
		if err != nil {
			errorsFound = append(errorsFound, err.Error())
			continue
		}
		hostsJSON, err := download.EncodeHeaderHosts(hosts)
		if err != nil {
			errorsFound = append(errorsFound, err.Error())
			continue
		}
		shouldSubmit := s.canSubmitItem(initial, submittedBefore+created, created, subscription)
		status := "deferred"
		thumbStatus := "none"
		if item.ThumbnailURL != "" {
			thumbStatus = "waiting_file"
		}
		if initial && !shouldSubmit {
			status = "ignored_initial"
			if thumbStatus == "waiting_file" {
				thumbStatus = "ignored"
			}
		}
		now := time.Now()
		record := &models.SubscriptionItem{ID: itemID, SubscriptionID: subscription.ID, SourceGeneration: subscription.SourceGeneration, ItemKey: key, ExternalID: item.ID, Title: item.Title, URL: item.URL, DownloadType: item.DownloadType, FileName: item.FileName, SavePath: path, ThumbnailURL: item.ThumbnailURL, RequestHeadersEncrypted: encrypted, HeaderHostsJSON: hostsJSON, HeadersDigest: headersDigest(headers, hosts), Status: status, ThumbnailStatus: thumbStatus, PublishedAt: item.PublishedAt, CreatedAt: now, UpdatedAt: now}
		if !shouldSubmit {
			if err := s.factory.DB().Create(record).Error; err != nil {
				errorsFound = append(errorsFound, err.Error())
				continue
			}
			skipped++
			continue
		}
		_, err = s.downloadService.CreateSubscriptionItemAndDownload(ctx, record, subscriptionDownloadInput(subscription.UserID, record))
		if err != nil {
			errorsFound = append(errorsFound, err.Error())
			continue
		}
		created++
	}
	if len(errorsFound) > 0 {
		return found, created, skipped, fmt.Errorf("%s", strings.Join(errorsFound, "; "))
	}
	return found, created, skipped, nil
}

func (s *SubscriptionService) canSubmitItem(initial bool, totalSubmitted, created int, subscription *models.Subscription) bool {
	if initial {
		return totalSubmitted < subscription.InitialLimit
	}
	return created < subscription.MaxItemsPerRun
}

func subscriptionDownloadInput(userID string, item *models.SubscriptionItem) SubscriptionDownloadInput {
	return SubscriptionDownloadInput{ItemID: item.ID, UserID: userID, URL: item.URL, DownloadType: item.DownloadType,
		FileName: item.FileName, SavePath: item.SavePath, RequestHeadersEncrypted: item.RequestHeadersEncrypted, HeaderHostsJSON: item.HeaderHostsJSON}
}

func (s *SubscriptionService) refreshExistingItem(ctx context.Context, subscription *models.Subscription, manifest *pluginpkg.Manifest, existing *models.SubscriptionItem, item pluginpkg.DownloadableItem, permissions map[string]bool) error {
	updates := map[string]interface{}{"updated_at": time.Now()}
	if existing.Status == "deferred" || existing.Status == "submit_failed" {
		path, err := virtualpath.JoinSubscriptionPath(subscription.DefaultPath, item.SavePath)
		if err != nil {
			return err
		}
		updates["save_path"] = path
	}
	if item.ThumbnailURL != "" && item.ThumbnailURL != existing.ThumbnailURL {
		if err := download.ValidatePublicHTTPURL(item.ThumbnailURL); err != nil {
			return fmt.Errorf("缩略图地址无效")
		}
		updates["thumbnail_url"] = item.ThumbnailURL
		updates["thumbnail_status"] = "waiting_file"
		updates["thumbnail_retry_count"] = 0
		updates["thumbnail_next_retry_at"] = nil
		updates["thumbnail_error"] = ""
	}
	if len(item.RequestHeaders) > 0 && (!manifest.HasPermission(pluginpkg.PermissionCustomHeaders) || !permissions[pluginpkg.PermissionCustomHeaders]) {
		return fmt.Errorf("插件未获自定义下载头权限")
	}
	if len(item.RequestHeaders) == 0 && len(item.HeaderHosts) > 0 {
		return fmt.Errorf("未配置请求头时不能单独配置请求头主机")
	}
	{
		headers, hosts, err := download.NormalizeRequestConfig(item.URL, item.RequestHeaders, item.HeaderHosts)
		if err != nil {
			return err
		}
		digest := headersDigest(headers, hosts)
		if digest != existing.HeadersDigest {
			encrypted, encryptErr := download.EncryptRequestHeaders(config.CONFIG.Auth.Secret, existing.ID, subscription.UserID, headers)
			if encryptErr != nil {
				return encryptErr
			}
			hostsJSON, encodeErr := download.EncodeHeaderHosts(hosts)
			if encodeErr != nil {
				return encodeErr
			}
			updates["request_headers_encrypted"] = encrypted
			updates["header_hosts_json"] = hostsJSON
			updates["headers_digest"] = digest
			if existing.DownloadTaskID != "" {
				if err := s.downloadService.RefreshTaskHeaders(ctx, existing.DownloadTaskID, existing.ID, subscription.UserID, encrypted, hostsJSON); err != nil {
					return err
				}
			}
		}
	}
	return s.factory.DB().Model(existing).Updates(updates).Error
}

func (s *SubscriptionService) queryFiles(ctx context.Context, subscription models.Subscription, plugin models.InstalledPlugin, request pluginpkg.FileQueryRequest) (pluginpkg.FileQueryResponse, error) {
	started := time.Now()
	response, err := s.queryFilesInternal(ctx, subscription.UserID, subscription.DefaultPath, request)
	status := "success"
	errorMsg := ""
	if err != nil {
		status = "failed"
		errorMsg = err.Error()
	}
	s.factory.DB().Create(&models.PluginAuditLog{ID: uuid.NewString(), PluginID: plugin.ID, PluginVersion: plugin.Version, SubscriptionID: subscription.ID, UserID: subscription.UserID, Action: "files." + request.Operation, Summary: fmt.Sprintf("path=%s limit=%d", request.Path, request.Limit), ResultCount: len(response.Files), DurationMS: time.Since(started).Milliseconds(), Status: status, ErrorMsg: errorMsg, CreatedAt: time.Now()})
	return response, err
}

func (s *SubscriptionService) queryFilesInternal(ctx context.Context, userID, saveRoot string, request pluginpkg.FileQueryRequest) (pluginpkg.FileQueryResponse, error) {
	scopeID, err := s.lookupPathID(ctx, userID, saveRoot)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if request.Operation == "get" {
				return pluginpkg.FileQueryResponse{}, fmt.Errorf("not_found")
			}
			return pluginpkg.FileQueryResponse{}, nil
		}
		return pluginpkg.FileQueryResponse{}, err
	}
	scopeIDs, err := s.descendantPathIDs(ctx, userID, scopeID)
	if err != nil {
		return pluginpkg.FileQueryResponse{}, err
	}
	scopedQuery := s.fileQueryBase(ctx, userID).Where("user_files.virtual_path IN ?", scopeIDs)
	if request.Operation == "get" {
		var row safeFileRow
		err := scopedQuery.Where("user_files.uf_id = ?", request.UFID).First(&row).Error
		if err != nil {
			return pluginpkg.FileQueryResponse{}, fmt.Errorf("not_found")
		}
		item, err := s.safeFileInfo(ctx, userID, row)
		return pluginpkg.FileQueryResponse{Files: []pluginpkg.SafeFileInfo{item}}, err
	}
	if request.Operation != "query" {
		return pluginpkg.FileQueryResponse{}, fmt.Errorf("invalid_request")
	}
	limit := request.Limit
	if limit < 1 || limit > 100 {
		limit = 100
	}
	query := scopedQuery
	if request.NameContains != "" {
		query = query.Where("user_files.file_name LIKE ?", "%"+request.NameContains+"%")
	}
	if request.MIMEPrefix != "" {
		query = query.Where("file_info.mime LIKE ?", request.MIMEPrefix+"%")
	}
	if request.IsEncrypted != nil {
		query = query.Where("file_info.is_enc = ?", *request.IsEncrypted)
	}
	if request.IsPublic != nil {
		query = query.Where("user_files.public = ?", *request.IsPublic)
	}
	if request.HasThumbnail != nil {
		if *request.HasThumbnail {
			query = query.Where("file_info.thumbnail_img <> ''")
		} else {
			query = query.Where("(file_info.thumbnail_img = '' OR file_info.thumbnail_img IS NULL)")
		}
	}
	if request.CreatedAfter != nil {
		query = query.Where("user_files.created_at >= ?", *request.CreatedAfter)
	}
	if request.CreatedBefore != nil {
		query = query.Where("user_files.created_at <= ?", *request.CreatedBefore)
	}
	if request.UpdatedAfter != nil {
		query = query.Where("file_info.updated_at >= ?", *request.UpdatedAfter)
	}
	if request.UpdatedBefore != nil {
		query = query.Where("file_info.updated_at <= ?", *request.UpdatedBefore)
	}
	queryPath, err := virtualpath.JoinSubscriptionPath(saveRoot, request.Path)
	if err != nil {
		return pluginpkg.FileQueryResponse{}, err
	}
	pathID, err := s.lookupPathID(ctx, userID, queryPath)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return pluginpkg.FileQueryResponse{}, nil
		}
		return pluginpkg.FileQueryResponse{}, err
	}
	if request.Recursive {
		pathIDs, descendantsErr := s.descendantPathIDs(ctx, userID, pathID)
		if descendantsErr != nil {
			return pluginpkg.FileQueryResponse{}, descendantsErr
		}
		query = query.Where("user_files.virtual_path IN ?", pathIDs)
	} else {
		query = query.Where("user_files.virtual_path = ?", pathID)
	}
	if request.Cursor != "" {
		createdAt, ufID, cursorErr := decodeFileCursor(userID, request.Cursor)
		if cursorErr != nil {
			return pluginpkg.FileQueryResponse{}, fmt.Errorf("invalid_cursor")
		}
		query = query.Where("(user_files.created_at < ?) OR (user_files.created_at = ? AND user_files.uf_id > ?)", createdAt, createdAt, ufID)
	}
	var rows []safeFileRow
	if err := query.Order("user_files.created_at DESC, user_files.uf_id ASC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return pluginpkg.FileQueryResponse{}, err
	}
	next := ""
	if len(rows) > limit {
		last := rows[limit-1]
		next = encodeFileCursor(userID, last.CreatedAt, last.UFID)
		rows = rows[:limit]
	}
	files := make([]pluginpkg.SafeFileInfo, 0, len(rows))
	for _, row := range rows {
		item, err := s.safeFileInfo(ctx, userID, row)
		if err == nil {
			files = append(files, item)
		}
	}
	return pluginpkg.FileQueryResponse{Files: files, NextCursor: next}, nil
}

func (s *SubscriptionService) descendantPathIDs(ctx context.Context, userID, rootID string) ([]string, error) {
	var paths []models.VirtualPath
	if err := s.factory.DB().WithContext(ctx).Where("user_id = ?", userID).Find(&paths).Error; err != nil {
		return nil, err
	}
	children := make(map[string][]string)
	for _, path := range paths {
		children[path.ParentLevel] = append(children[path.ParentLevel], strconv.Itoa(path.ID))
	}
	result := []string{rootID}
	for index := 0; index < len(result); index++ {
		result = append(result, children[result[index]]...)
		if len(result) > 10000 {
			return nil, fmt.Errorf("目录层级数据异常")
		}
	}
	return result, nil
}

func encodeFileCursor(userID string, createdAt time.Time, ufID string) string {
	payload := []byte(createdAt.UTC().Format(time.RFC3339Nano) + "\x00" + ufID)
	mac := hmac.New(sha256.New, []byte(config.CONFIG.Auth.Secret))
	mac.Write([]byte("subscription-file-cursor-v1\x00" + userID + "\x00"))
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...))
}

func decodeFileCursor(userID, cursor string) (time.Time, string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(decoded) <= sha256.Size {
		return time.Time{}, "", fmt.Errorf("invalid_cursor")
	}
	payload, signature := decoded[:len(decoded)-sha256.Size], decoded[len(decoded)-sha256.Size:]
	mac := hmac.New(sha256.New, []byte(config.CONFIG.Auth.Secret))
	mac.Write([]byte("subscription-file-cursor-v1\x00" + userID + "\x00"))
	mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return time.Time{}, "", fmt.Errorf("invalid_cursor")
	}
	parts := strings.SplitN(string(payload), "\x00", 2)
	if len(parts) != 2 || parts[1] == "" {
		return time.Time{}, "", fmt.Errorf("invalid_cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	return createdAt, parts[1], err
}

type safeFileRow struct {
	UFID         string
	FileName     string
	VirtualPath  string
	FileSize     int64
	MIMEType     string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	IsEncrypted  bool
	IsPublic     bool
	ThumbnailImg string
}

func (s *SubscriptionService) fileQueryBase(ctx context.Context, userID string) *gorm.DB {
	return s.factory.DB().WithContext(ctx).Table("user_files").Select("user_files.uf_id,user_files.file_name,user_files.virtual_path,file_info.size AS file_size,file_info.mime AS mime_type,user_files.created_at,file_info.updated_at,file_info.is_enc AS is_encrypted,user_files.public AS is_public,file_info.thumbnail_img").Joins("JOIN file_info ON file_info.id = user_files.file_id").Where("user_files.user_id = ? AND user_files.deleted_at IS NULL", userID)
}
func (s *SubscriptionService) safeFileInfo(ctx context.Context, userID string, row safeFileRow) (pluginpkg.SafeFileInfo, error) {
	path, err := virtualpath.ResolveAbsolute(ctx, userID, row.VirtualPath, s.factory)
	return pluginpkg.SafeFileInfo{UFID: row.UFID, FileName: row.FileName, VirtualPath: path, FileSize: row.FileSize, MIMEType: row.MIMEType, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, IsEncrypted: row.IsEncrypted, IsPublic: row.IsPublic, HasThumbnail: row.ThumbnailImg != ""}, err
}
func (s *SubscriptionService) lookupPathID(ctx context.Context, userID, raw string) (string, error) {
	normalized, err := virtualpath.Normalize(raw)
	if err != nil {
		return "", err
	}
	root, err := s.factory.VirtualPath().GetRootPath(ctx, userID)
	if err != nil {
		return "", err
	}
	id := strconv.Itoa(root.ID)
	if normalized == "/" {
		return id, nil
	}
	for _, part := range strings.Split(strings.TrimPrefix(normalized, "/"), "/") {
		var path models.VirtualPath
		err = s.factory.DB().WithContext(ctx).Where("user_id = ? AND parent_level = ? AND path = ?", userID, id, "/"+part).First(&path).Error
		if err != nil {
			return "", err
		}
		id = strconv.Itoa(path.ID)
	}
	return id, nil
}

func (s *SubscriptionService) checkUserPermission(ctx context.Context, userID string) error {
	user, err := s.factory.User().GetByID(ctx, userID)
	if err != nil || user.State != 0 {
		return fmt.Errorf("用户不存在或已停用")
	}
	var count int64
	err = s.factory.DB().WithContext(ctx).Table("group_power").Joins("JOIN power ON power.id = group_power.power_id").Where("group_power.group_id = ? AND power.characteristic = ?", user.GroupID, "file:offLine").Count(&count).Error
	if err != nil || count == 0 {
		return fmt.Errorf("用户已无离线下载权限")
	}
	return nil
}
func (s *SubscriptionService) failRun(run *models.SubscriptionRun, err error) {
	now := time.Now()
	query := s.factory.DB().Model(&models.SubscriptionRun{}).Where("id = ?", run.ID)
	if run.RunToken != "" {
		query = query.Where("run_token = ?", run.RunToken)
	}
	query.Updates(map[string]interface{}{"status": "failed", "error_msg": err.Error(), "finished_at": now, "lease_expires_at": nil, "run_token": ""})
	s.factory.DB().Model(&models.Subscription{}).Where("id = ?", run.SubscriptionID).Updates(map[string]interface{}{"last_error": err.Error(), "updated_at": now})
}
func (s *SubscriptionService) cancelActive(id string) {
	s.activeMu.Lock()
	cancel := s.active[id]
	s.activeMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *SubscriptionService) cancelPluginRuns(pluginID string) {
	var subscriptions []models.Subscription
	if err := s.factory.DB().Where("plugin_id = ?", pluginID).Find(&subscriptions).Error; err != nil {
		return
	}
	for _, subscription := range subscriptions {
		s.cancelActive(subscription.ID)
	}
}

func (s *SubscriptionService) heartbeatRun(ctx context.Context, runID, token string, done <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			_ = s.factory.DB().Model(&models.SubscriptionRun{}).
				Where("id = ? AND run_token = ? AND status = ?", runID, token, "running").
				Update("lease_expires_at", time.Now().Add(2*time.Minute)).Error
		}
	}
}

func (s *SubscriptionService) recoverInterruptedRuns() {
	var queued []models.SubscriptionRun
	if err := s.factory.DB().Where("status = ?", "queued").Order("created_at ASC").Find(&queued).Error; err == nil {
		for _, run := range queued {
			runID := run.ID
			go func() {
				s.sem <- struct{}{}
				defer func() { <-s.sem }()
				s.executeRun(runID)
			}()
		}
	}
	var running []models.SubscriptionRun
	if err := s.factory.DB().Where("status = ?", "running").Find(&running).Error; err != nil {
		return
	}
	for _, run := range running {
		now := time.Now()
		s.factory.DB().Model(&models.SubscriptionRun{}).Where("id = ? AND status = ?", run.ID, "running").Updates(map[string]interface{}{
			"status": "interrupted", "error_msg": "服务重启后恢复", "finished_at": now, "run_token": "", "lease_expires_at": nil,
		})
		var subscription models.Subscription
		if err := s.factory.DB().Where("id = ? AND enabled = ? AND status = ?", run.SubscriptionID, true, "ready").First(&subscription).Error; err == nil {
			next, scheduleErr := nextScheduleInLocation(subscription.ScheduleTime, now, s.location)
			if scheduleErr == nil {
				s.factory.DB().Model(&models.Subscription{}).Where("id = ?", subscription.ID).Update("next_run_at", next)
			}
			s.queueRun(subscription.ID, "recovery")
		}
	}
}

func (s *SubscriptionService) writeAudit(subscription models.Subscription, plugin models.InstalledPlugin, action string, count int, duration time.Duration, status, errorMsg string) {
	_ = s.factory.DB().Create(&models.PluginAuditLog{ID: uuid.NewString(), PluginID: plugin.ID, PluginVersion: plugin.Version,
		SubscriptionID: subscription.ID, UserID: subscription.UserID, Action: action, Summary: "subscription_run",
		ResultCount: count, DurationMS: duration.Milliseconds(), Status: status, ErrorMsg: errorMsg, CreatedAt: time.Now()}).Error
}

func nextSchedule(value string, now time.Time) (time.Time, error) {
	return nextScheduleInLocation(value, now, subscriptionLocation())
}

func nextScheduleInLocation(value string, now time.Time, location *time.Location) (time.Time, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("执行时间必须为HH:mm")
	}
	now = now.In(location)
	next := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, location)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next, nil
}

func subscriptionLocation() *time.Location {
	name := "Asia/Shanghai"
	if config.CONFIG != nil && strings.TrimSpace(config.CONFIG.Server.Timezone) != "" {
		name = strings.TrimSpace(config.CONFIG.Server.Timezone)
	}
	location, err := time.LoadLocation(name)
	if err == nil {
		return location
	}
	if name == "Asia/Shanghai" {
		return time.FixedZone(name, 8*60*60)
	}
	return time.Local
}
func validateGrantedPermissions(manifest *pluginpkg.Manifest, values []string) ([]string, error) {
	seen := map[string]bool{}
	result := []string{}
	if manifest.HasPermission(pluginpkg.PermissionPublicHTTP) {
		seen[pluginpkg.PermissionPublicHTTP] = true
		result = append(result, pluginpkg.PermissionPublicHTTP)
	}
	for _, value := range values {
		if !manifest.HasPermission(value) {
			return nil, fmt.Errorf("插件未声明权限: %s", value)
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result, nil
}

func sourceConfigChanged(manifest *pluginpkg.Manifest, before, after map[string]interface{}) bool {
	for _, field := range manifest.ConfigFields {
		if field.AffectsSource && !reflect.DeepEqual(before[field.Key], after[field.Key]) {
			return true
		}
	}
	return false
}
func decodePermissions(value string) []string {
	var result []string
	_ = json.Unmarshal([]byte(value), &result)
	return result
}
func permissionMap(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}
func itemKey(item pluginpkg.DownloadableItem) string {
	identity := strings.TrimSpace(item.ID)
	if identity != "" {
		digest := sha256.Sum256([]byte("id:" + identity))
		return hex.EncodeToString(digest[:])
	}
	normalized := strings.TrimSpace(item.URL)
	if parsed, err := url.Parse(item.URL); err == nil {
		parsed.Fragment = ""
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		parsed.Host = strings.ToLower(parsed.Host)
		if parsed.Path == "" {
			parsed.Path = "/"
		}
		normalized = parsed.String()
	}
	digest := sha256.Sum256([]byte("url:" + normalized))
	return hex.EncodeToString(digest[:])
}
func headersDigest(headers map[string]string, hosts []string) string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sort.Strings(hosts)
	mac := hmac.New(sha256.New, []byte(config.CONFIG.Auth.Secret))
	for _, key := range keys {
		mac.Write([]byte(key))
		mac.Write([]byte{0})
		mac.Write([]byte(headers[key]))
		mac.Write([]byte{0})
	}
	for _, host := range hosts {
		mac.Write([]byte(host))
		mac.Write([]byte{0})
	}
	return hex.EncodeToString(mac.Sum(nil))
}
