package virtualpath

import (
	"context"
	"errors"
	"fmt"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
	"regexp"
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
var windowsDrivePathPattern = regexp.MustCompile(`^/?[A-Za-z]:($|/)`)

// NormalizeDirectoryName 规范单级目录名称。目录名称不是路径，因此不能包含分隔符。
func NormalizeDirectoryName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("目录名称不能为空")
	}
	if name == "." || name == ".." {
		return "", fmt.Errorf("目录名称不能是.或..")
	}
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("目录名称不能包含路径分隔符")
	}
	if len([]rune(name)) > MaxSegmentLength {
		return "", fmt.Errorf("目录名称不能超过%d个字符", MaxSegmentLength)
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return "", fmt.Errorf("目录名称不能包含控制字符")
		}
	}
	return name, nil
}

func normalizeSegments(raw string, absolute bool) ([]string, error) {
	value := strings.TrimSpace(raw)
	if absolute {
		if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
			return nil, fmt.Errorf("保存目录必须是以/开头的用户虚拟绝对路径")
		}
	} else if strings.HasPrefix(value, "/") {
		return nil, fmt.Errorf("相对目录不能以/开头")
	}
	if strings.Contains(value, "\\") || strings.Contains(value, "://") || windowsDrivePathPattern.MatchString(value) {
		return nil, fmt.Errorf("虚拟路径格式无效")
	}
	parts := make([]string, 0)
	for _, part := range strings.Split(value, "/") {
		if part == "" {
			continue
		}
		name, err := NormalizeDirectoryName(part)
		if err != nil {
			return nil, err
		}
		parts = append(parts, name)
	}
	if len(parts) > MaxDepth {
		return nil, fmt.Errorf("虚拟路径不能超过%d层", MaxDepth)
	}
	return parts, nil
}

// NormalizeAbsolutePath 规范用户虚拟绝对路径。根目录固定为/，其他路径不保留尾随/。
func NormalizeAbsolutePath(raw string) (string, error) {
	parts, err := normalizeSegments(raw, true)
	if err != nil {
		return "", err
	}
	result := "/" + strings.Join(parts, "/")
	if len([]rune(result)) > MaxPathLength {
		return "", fmt.Errorf("虚拟路径不能超过%d个字符", MaxPathLength)
	}
	return result, nil
}

// NormalizeRelativePath 规范相对目录。空值表示基准目录。
func NormalizeRelativePath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	parts, err := normalizeSegments(raw, false)
	if err != nil {
		return "", err
	}
	result := strings.Join(parts, "/")
	if len([]rune(result)) > MaxPathLength {
		return "", fmt.Errorf("相对目录不能超过%d个字符", MaxPathLength)
	}
	return result, nil
}

// JoinSavePath 将相对目录拼接到保存目录下。
func JoinSavePath(saveRoot, relativePath string) (string, error) {
	root, err := NormalizeAbsolutePath(saveRoot)
	if err != nil {
		return "", err
	}
	relative, err := NormalizeRelativePath(relativePath)
	if err != nil {
		return "", err
	}
	if relative == "" {
		return root, nil
	}
	if root == "/" {
		return NormalizeAbsolutePath("/" + relative)
	}
	return NormalizeAbsolutePath(root + "/" + relative)
}

// FindChildDirectory 在指定父目录下按规范名称查找直接子目录。
func FindChildDirectory(ctx context.Context, userID string, parentID int, name string, factory *impl.RepositoryFactory) (*models.VirtualDirectory, error) {
	canonicalName, err := NormalizeDirectoryName(name)
	if err != nil {
		return nil, err
	}
	var directory models.VirtualDirectory
	err = factory.DB().WithContext(ctx).
		Where("user_id = ? AND parent_id = ? AND name = ?", userID, parentID, canonicalName).
		First(&directory).Error
	if err != nil {
		return nil, err
	}
	return &directory, nil
}

// ResolveDirectoryID 将虚拟绝对路径解析为目录ID。
func ResolveDirectoryID(ctx context.Context, userID, raw string, factory *impl.RepositoryFactory) (int, error) {
	normalized, err := NormalizeAbsolutePath(raw)
	if err != nil {
		return 0, err
	}
	root, err := factory.Directory().GetRoot(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("获取用户根目录失败: %w", err)
	}
	currentID := root.ID
	if normalized == "/" {
		return currentID, nil
	}
	for _, part := range strings.Split(strings.TrimPrefix(normalized, "/"), "/") {
		child, findErr := FindChildDirectory(ctx, userID, currentID, part, factory)
		if findErr != nil {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				return 0, fmt.Errorf("目录不存在: %s: %w", normalized, gorm.ErrRecordNotFound)
			}
			return 0, findErr
		}
		currentID = child.ID
	}
	return currentID, nil
}

// EnsureDirectory 确保虚拟绝对路径对应的目录树存在，并返回最终目录ID。
func EnsureDirectory(ctx context.Context, userID, raw string, factory *impl.RepositoryFactory) (int, error) {
	normalized, err := NormalizeAbsolutePath(raw)
	if err != nil {
		return 0, err
	}
	root, err := factory.Directory().GetRoot(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("获取用户根目录失败: %w", err)
	}
	if normalized == "/" {
		return root.ID, nil
	}
	lockValue, _ := ensureLocks.LoadOrStore(userID, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	parentID := root.ID
	for _, part := range strings.Split(strings.TrimPrefix(normalized, "/"), "/") {
		current, findErr := FindChildDirectory(ctx, userID, parentID, part, factory)
		if findErr == nil {
			parentID = current.ID
			continue
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return 0, findErr
		}
		now := custom_type.Now()
		created := models.VirtualDirectory{UserID: userID, Name: part, ParentID: parentID, CreatedAt: now, UpdatedAt: now}
		if err := factory.Directory().Create(ctx, &created); err != nil {
			if current, queryErr := FindChildDirectory(ctx, userID, parentID, part, factory); queryErr == nil {
				parentID = current.ID
				continue
			}
			return 0, err
		}
		parentID = created.ID
	}
	return parentID, nil
}

// ResolveAbsolutePath 将目录ID解析为用户虚拟绝对路径。
func ResolveAbsolutePath(ctx context.Context, userID string, directoryID int, factory *impl.RepositoryFactory) (string, error) {
	if directoryID <= 0 {
		return "", fmt.Errorf("目录ID无效")
	}
	parts := make([]string, 0)
	visited := map[int]struct{}{}
	currentID := directoryID
	for depth := 0; depth <= MaxDepth; depth++ {
		if _, exists := visited[currentID]; exists {
			return "", fmt.Errorf("目录树包含循环")
		}
		visited[currentID] = struct{}{}
		directory, err := factory.Directory().GetByID(ctx, currentID)
		if err != nil || directory.UserID != userID {
			return "", fmt.Errorf("目录不存在")
		}
		if directory.ParentID == 0 {
			if directory.Name != "" {
				return "", fmt.Errorf("根目录数据无效")
			}
			return "/" + strings.Join(parts, "/"), nil
		}
		parts = append([]string{directory.Name}, parts...)
		currentID = directory.ParentID
	}
	return "", fmt.Errorf("目录层级超过%d层", MaxDepth)
}
