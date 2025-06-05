package rdp

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
)

// SessionStore 会话存储 - 参考mayfly-go的MemorySessionStore
type SessionStore struct {
	sessions    map[string]*Session
	mutex       sync.RWMutex
	timeout     time.Duration
	cleanupStop chan struct{}
	logger      logx.Logger
}

// Session 会话信息
type Session struct {
	ID           string
	TunnelUUID   string
	WSConnection *websocket.Conn
	Request      *http.Request
	Tunnel       Tunnel
	CreatedAt    time.Time
	LastActivity time.Time
	mutex        sync.RWMutex
}

// NewSessionStore 创建会话存储
func NewSessionStore() *SessionStore {
	store := &SessionStore{
		sessions:    make(map[string]*Session),
		timeout:     2 * time.Hour, // 2小时超时
		cleanupStop: make(chan struct{}),
		logger:      logx.WithContext(nil),
	}

	// 启动清理协程
	go store.cleanupRoutine()

	return store
}

// Add 添加会话 - 参考mayfly-go的Add方法
func (s *SessionStore) Add(id string, conn *websocket.Conn, req *http.Request, tunnel Tunnel) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// 如果已存在同ID会话，先关闭
	if existing, exists := s.sessions[id]; exists {
		existing.Close()
	}

	session := &Session{
		ID:           id,
		TunnelUUID:   tunnel.GetUUID(),
		WSConnection: conn,
		Request:      req,
		Tunnel:       tunnel,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}

	s.sessions[id] = session
	s.logger.Infof("Added session: %s", id)
}

// Get 获取会话 - 参考mayfly-go的Get方法
func (s *SessionStore) Get(id string) *Session {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if session, exists := s.sessions[id]; exists {
		session.UpdateActivity()
		return session
	}
	return nil
}

// Delete 删除会话 - 参考mayfly-go的Delete方法
func (s *SessionStore) Delete(id string, conn *websocket.Conn, req *http.Request, tunnel Tunnel) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if session, exists := s.sessions[id]; exists {
		session.Close()
		delete(s.sessions, id)
		s.logger.Infof("Deleted session: %s", id)
	}
}

// List 列出所有会话ID
func (s *SessionStore) List() []string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	return ids
}

// CloseAll 关闭所有会话
func (s *SessionStore) CloseAll() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	close(s.cleanupStop)

	for id, session := range s.sessions {
		session.Close()
		s.logger.Infof("Closed session: %s", id)
	}

	s.sessions = make(map[string]*Session)
	s.logger.Info("Closed all sessions")
}

// cleanupRoutine 清理过期会话
func (s *SessionStore) cleanupRoutine() {
	ticker := time.NewTicker(10 * time.Minute) // 每10分钟检查一次
	defer ticker.Stop()

	for {
		select {
		case <-s.cleanupStop:
			return
		case <-ticker.C:
			s.cleanupExpiredSessions()
		}
	}
}

// cleanupExpiredSessions 清理过期会话
func (s *SessionStore) cleanupExpiredSessions() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now()
	expired := make([]string, 0)

	for id, session := range s.sessions {
		if now.Sub(session.GetLastActivity()) > s.timeout {
			expired = append(expired, id)
		}
	}

	for _, id := range expired {
		if session := s.sessions[id]; session != nil {
			session.Close()
			delete(s.sessions, id)
			s.logger.Infof("Cleaned up expired session: %s", id)
		}
	}
}

// GetStats 获取统计信息
func (s *SessionStore) GetStats() map[string]interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return map[string]interface{}{
		"total_sessions": len(s.sessions),
		"timeout":        s.timeout.String(),
	}
}

// Session methods

// UpdateActivity 更新活动时间
func (s *Session) UpdateActivity() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.LastActivity = time.Now()
}

// GetLastActivity 获取最后活动时间
func (s *Session) GetLastActivity() time.Time {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.LastActivity
}

// Close 关闭会话
func (s *Session) Close() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.WSConnection != nil {
		s.WSConnection.Close()
	}

	if s.Tunnel != nil {
		s.Tunnel.Close()
	}
}
