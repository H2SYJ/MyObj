package cache

import (
	"errors"
	"myobj/src/config"
	"myobj/src/pkg/logger"
)

var (
	ErrKeyNotFound = errors.New("缓存键不存在")
	ErrKeyExpired  = errors.New("缓存键已过期")
)

// Cache 缓存接口定义
type Cache interface {
	Get(key string) (any, error)
	Set(key string, value any, expire int) error
	Delete(key string) error
	Stop()
}

func InitCache() Cache {
	cfg := config.GetConfig().Cache
	if cfg.Type == "redis" {
		logger.LOG.Info("使用 Redis 缓存")
		return NewRedisCache(&cfg)
	}
	return NewLocalCache()
}
