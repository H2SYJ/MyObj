package virtualpath

import (
	"context"
	"errors"
	"fmt"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"gorm.io/gorm"
)

const (
	MaxDepth         = 20
	MaxSegmentLength = 100
	MaxPathLength    = 1000
)

var ensureLocks sync.Map
var windowsDrivePathPattern = regexp.MustCompile(`^/[A-Za-z]:($|/)`)

func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("保存目录不能为空")
	}
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.Contains(raw, "\\") || strings.Contains(raw, "://") || windowsDrivePathPattern.MatchString(raw) {
		return "", fmt.Errorf("保存目录必须是以/开头的用户虚拟绝对路径")
	}
	parts := strings.Split(raw, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if part == "." || part == ".." {
			return "", fmt.Errorf("保存目录不能包含.或..")
		}
		if len([]rune(part)) > MaxSegmentLength {
			return "", fmt.Errorf("目录名称不能超过%d个字符", MaxSegmentLength)
		}
		for _, char := range part {
			if unicode.IsControl(char) {
				return "", fmt.Errorf("保存目录不能包含控制字符")
			}
		}
		cleaned = append(cleaned, part)
	}
	if len(cleaned) > MaxDepth {
		return "", fmt.Errorf("保存目录不能超过%d层", MaxDepth)
	}
	result := "/" + strings.Join(cleaned, "/")
	if len([]rune(result)) > MaxPathLength {
		return "", fmt.Errorf("保存目录不能超过%d个字符", MaxPathLength)
	}
	return result, nil
}

// JoinSubscriptionPath 将插件返回的根相对目录拼接到订阅保存目录下。
// 插件目录为空或为 / 时，结果就是订阅保存目录。
func JoinSubscriptionPath(saveRoot, pluginPath string) (string, error) {
	normalizedRoot, err := Normalize(saveRoot)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(pluginPath) == "" {
		return normalizedRoot, nil
	}
	normalizedPluginPath, err := Normalize(pluginPath)
	if err != nil {
		return "", err
	}
	if normalizedPluginPath == "/" {
		return normalizedRoot, nil
	}
	if normalizedRoot == "/" {
		return normalizedPluginPath, nil
	}
	return Normalize(normalizedRoot + normalizedPluginPath)
}

func Ensure(ctx context.Context, userID, raw string, factory *impl.RepositoryFactory) (string, error) {
	normalized, err := Normalize(raw)
	if err != nil {
		return "", err
	}
	root, err := factory.VirtualPath().GetRootPath(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("获取用户根目录失败: %w", err)
	}
	if normalized == "/" {
		return strconv.Itoa(root.ID), nil
	}
	// 同一用户串行创建目录，避免不同完整路径共享前缀时重复创建中间目录。
	lockValue, _ := ensureLocks.LoadOrStore(userID, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	parentID := strconv.Itoa(root.ID)
	for _, part := range strings.Split(strings.TrimPrefix(normalized, "/"), "/") {
		var current models.VirtualPath
		err := factory.DB().WithContext(ctx).
			Where("user_id = ? AND parent_level = ? AND path = ? AND is_dir = ?", userID, parentID, "/"+part, true).
			First(&current).Error
		if err == nil {
			parentID = strconv.Itoa(current.ID)
			continue
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", err
		}
		current = models.VirtualPath{UserID: userID, Path: "/" + part, IsDir: true, ParentLevel: parentID, CreatedTime: custom_type.Now(), UpdateTime: custom_type.Now()}
		if err := factory.VirtualPath().Create(ctx, &current); err != nil {
			return "", err
		}
		parentID = strconv.Itoa(current.ID)
	}
	return parentID, nil
}

func ResolveAbsolute(ctx context.Context, userID, pathID string, factory *impl.RepositoryFactory) (string, error) {
	if pathID == "" || pathID == "0" {
		return "/", nil
	}
	id, err := strconv.Atoi(pathID)
	if err != nil {
		return "", err
	}
	parts := []string{}
	for depth := 0; depth <= MaxDepth; depth++ {
		path, err := factory.VirtualPath().GetByID(ctx, id)
		if err != nil || path.UserID != userID {
			return "", fmt.Errorf("目录不存在")
		}
		if path.ParentLevel == "" {
			break
		}
		parts = append([]string{strings.Trim(path.Path, "/")}, parts...)
		id, err = strconv.Atoi(path.ParentLevel)
		if err != nil {
			return "", err
		}
	}
	return "/" + strings.Join(parts, "/"), nil
}
