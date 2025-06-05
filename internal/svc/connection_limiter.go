package svc

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// ConnectionLimiter 连接限制器
type ConnectionLimiter struct {
	maxConnections int32
	activeCount    int32
	connections    map[string]*ConnectionInfo
	mutex          sync.RWMutex
	logger         logx.Logger
}

// ConnectionInfo 连接信息
type ConnectionInfo struct {
	ID         string
	CreatedAt  time.Time
	LastUsed   time.Time
	IsActive   bool
	RemoteAddr string
	Protocol   string
	mutex      sync.RWMutex
}

// NewConnectionLimiter 创建连接限制器
func NewConnectionLimiter(maxConnections int) *ConnectionLimiter {
	return &ConnectionLimiter{
		maxConnections: int32(maxConnections),
		connections:    make(map[string]*ConnectionInfo),
		logger:         logx.WithContext(nil),
	}
}

// AcquireConnection 获取连接许可
func (cl *ConnectionLimiter) AcquireConnection(id, remoteAddr, protocol string) bool {
	if atomic.LoadInt32(&cl.activeCount) >= cl.maxConnections {
		cl.logger.Errorf("连接数已达上限: %d", cl.maxConnections)
		return false
	}

	cl.mutex.Lock()
	defer cl.mutex.Unlock()

	// 检查连接是否已存在
	if _, exists := cl.connections[id]; exists {
		cl.logger.Errorf("连接ID已存在: %s", id)
		return false
	}

	// 创建连接信息
	connInfo := &ConnectionInfo{
		ID:         id,
		CreatedAt:  time.Now(),
		LastUsed:   time.Now(),
		IsActive:   true,
		RemoteAddr: remoteAddr,
		Protocol:   protocol,
	}

	cl.connections[id] = connInfo
	atomic.AddInt32(&cl.activeCount, 1)

	cl.logger.Infof("获取连接许可成功: %s, 当前连接数: %d", id, cl.activeCount)
	return true
}

// ReleaseConnection 释放连接
func (cl *ConnectionLimiter) ReleaseConnection(id string) {
	cl.mutex.Lock()
	defer cl.mutex.Unlock()

	if connInfo, exists := cl.connections[id]; exists {
		connInfo.mutex.Lock()
		connInfo.IsActive = false
		connInfo.mutex.Unlock()

		delete(cl.connections, id)
		atomic.AddInt32(&cl.activeCount, -1)

		cl.logger.Infof("释放连接: %s, 当前连接数: %d", id, cl.activeCount)
	}
}

// UpdateLastUsed 更新最后使用时间
func (cl *ConnectionLimiter) UpdateLastUsed(id string) {
	cl.mutex.RLock()
	defer cl.mutex.RUnlock()

	if connInfo, exists := cl.connections[id]; exists {
		connInfo.mutex.Lock()
		connInfo.LastUsed = time.Now()
		connInfo.mutex.Unlock()
	}
}

// GetActiveCount 获取活跃连接数
func (cl *ConnectionLimiter) GetActiveCount() int {
	return int(atomic.LoadInt32(&cl.activeCount))
}

// GetConnectionInfo 获取连接信息
func (cl *ConnectionLimiter) GetConnectionInfo(id string) (*ConnectionInfo, bool) {
	cl.mutex.RLock()
	defer cl.mutex.RUnlock()

	if connInfo, exists := cl.connections[id]; exists {
		// 返回副本以避免并发问题
		connInfo.mutex.RLock()
		copy := &ConnectionInfo{
			ID:         connInfo.ID,
			CreatedAt:  connInfo.CreatedAt,
			LastUsed:   connInfo.LastUsed,
			IsActive:   connInfo.IsActive,
			RemoteAddr: connInfo.RemoteAddr,
			Protocol:   connInfo.Protocol,
		}
		connInfo.mutex.RUnlock()
		return copy, true
	}

	return nil, false
}

// CleanupIdleConnections 清理空闲连接
func (cl *ConnectionLimiter) CleanupIdleConnections() int {
	cl.mutex.Lock()
	defer cl.mutex.Unlock()

	now := time.Now()
	idleTimeout := 30 * time.Minute
	toRemove := make([]string, 0)

	for id, connInfo := range cl.connections {
		connInfo.mutex.RLock()
		if now.Sub(connInfo.LastUsed) > idleTimeout {
			toRemove = append(toRemove, id)
		}
		connInfo.mutex.RUnlock()
	}

	for _, id := range toRemove {
		if connInfo, exists := cl.connections[id]; exists {
			connInfo.mutex.Lock()
			connInfo.IsActive = false
			connInfo.mutex.Unlock()

			delete(cl.connections, id)
			atomic.AddInt32(&cl.activeCount, -1)
		}
	}

	if len(toRemove) > 0 {
		cl.logger.Infof("清理了 %d 个空闲连接", len(toRemove))
	}

	return len(toRemove)
}

// GetAllConnections 获取所有连接信息
func (cl *ConnectionLimiter) GetAllConnections() []*ConnectionInfo {
	cl.mutex.RLock()
	defer cl.mutex.RUnlock()

	connections := make([]*ConnectionInfo, 0, len(cl.connections))
	for _, connInfo := range cl.connections {
		connInfo.mutex.RLock()
		copy := &ConnectionInfo{
			ID:         connInfo.ID,
			CreatedAt:  connInfo.CreatedAt,
			LastUsed:   connInfo.LastUsed,
			IsActive:   connInfo.IsActive,
			RemoteAddr: connInfo.RemoteAddr,
			Protocol:   connInfo.Protocol,
		}
		connInfo.mutex.RUnlock()
		connections = append(connections, copy)
	}

	return connections
}

// GetStats 获取统计信息
func (cl *ConnectionLimiter) GetStats() *ConnectionStats {
	cl.mutex.RLock()
	defer cl.mutex.RUnlock()

	stats := &ConnectionStats{
		MaxConnections:    int(cl.maxConnections),
		ActiveConnections: int(atomic.LoadInt32(&cl.activeCount)),
		TotalConnections:  len(cl.connections),
		ProtocolStats:     make(map[string]int),
	}

	for _, connInfo := range cl.connections {
		connInfo.mutex.RLock()
		stats.ProtocolStats[connInfo.Protocol]++
		connInfo.mutex.RUnlock()
	}

	return stats
}

// ConnectionStats 连接统计信息
type ConnectionStats struct {
	MaxConnections    int            `json:"max_connections"`
	ActiveConnections int            `json:"active_connections"`
	TotalConnections  int            `json:"total_connections"`
	ProtocolStats     map[string]int `json:"protocol_stats"`
}
