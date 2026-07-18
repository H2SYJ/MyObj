package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"myobj/src/pkg/cache"
	"sync"
)

var ErrSessionNotFound = errors.New("登录会话不存在或已过期")

type SessionRecord struct {
	JWT    string `json:"jwt"`
	UserID string `json:"user_id"`
}

// SessionStore 使用稳定的匿名会话ID保存JWT，并维护用户会话索引。
type SessionStore struct {
	cache cache.Cache
}

var sessionIndexMu sync.Mutex

func NewSessionStore(cacheStore cache.Cache) *SessionStore {
	return &SessionStore{cache: cacheStore}
}

func sessionKey(sessionID string) string {
	return "auth:session:" + sessionID
}

func userSessionsKey(userID string) string {
	return "auth:user_sessions:" + userID
}

func (s *SessionStore) Create(sessionID, userID, jwtToken string, ttlSeconds int) error {
	recordJSON, err := json.Marshal(SessionRecord{JWT: jwtToken, UserID: userID})
	if err != nil {
		return err
	}
	if err := s.cache.Set(sessionKey(sessionID), string(recordJSON), ttlSeconds); err != nil {
		return err
	}
	if err := s.addUserSession(userID, sessionID, ttlSeconds); err != nil {
		_ = s.cache.Delete(sessionKey(sessionID))
		return err
	}
	return nil
}

func (s *SessionStore) Get(sessionID string) (*SessionRecord, error) {
	value, err := s.cache.Get(sessionKey(sessionID))
	if err != nil {
		if errors.Is(err, cache.ErrKeyNotFound) || errors.Is(err, cache.ErrKeyExpired) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	serialized, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("登录会话格式错误: %T", value)
	}
	var record SessionRecord
	if err := json.Unmarshal([]byte(serialized), &record); err != nil {
		return nil, fmt.Errorf("解析登录会话失败: %w", err)
	}
	if record.JWT == "" || record.UserID == "" {
		return nil, fmt.Errorf("登录会话内容不完整")
	}
	return &record, nil
}

func (s *SessionStore) Refresh(sessionID string, record *SessionRecord, ttlSeconds int) error {
	serialized, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := s.cache.Set(sessionKey(sessionID), string(serialized), ttlSeconds); err != nil {
		return err
	}
	return s.addUserSession(record.UserID, sessionID, ttlSeconds)
}

func (s *SessionStore) Delete(sessionID string) error {
	return s.cache.Delete(sessionKey(sessionID))
}

func (s *SessionStore) RevokeUser(userID string) error {
	sessionIndexMu.Lock()
	defer sessionIndexMu.Unlock()
	ids, err := s.getUserSessions(userID)
	if err != nil && !errors.Is(err, ErrSessionNotFound) {
		return err
	}
	for _, sessionID := range ids {
		if deleteErr := s.cache.Delete(sessionKey(sessionID)); deleteErr != nil {
			return deleteErr
		}
	}
	return s.cache.Delete(userSessionsKey(userID))
}

func (s *SessionStore) addUserSession(userID, sessionID string, ttlSeconds int) error {
	sessionIndexMu.Lock()
	defer sessionIndexMu.Unlock()
	ids, err := s.getUserSessions(userID)
	if err != nil && !errors.Is(err, ErrSessionNotFound) {
		return err
	}
	found := false
	for _, id := range ids {
		if id == sessionID {
			found = true
			break
		}
	}
	if !found {
		ids = append(ids, sessionID)
	}
	serialized, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return s.cache.Set(userSessionsKey(userID), string(serialized), ttlSeconds)
}

func (s *SessionStore) getUserSessions(userID string) ([]string, error) {
	value, err := s.cache.Get(userSessionsKey(userID))
	if err != nil {
		if errors.Is(err, cache.ErrKeyNotFound) || errors.Is(err, cache.ErrKeyExpired) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	serialized, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("用户会话索引格式错误: %T", value)
	}
	var ids []string
	if err := json.Unmarshal([]byte(serialized), &ids); err != nil {
		return nil, fmt.Errorf("解析用户会话索引失败: %w", err)
	}
	return ids, nil
}
