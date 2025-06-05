package svc

import (
	"runtime"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// MemoryManager 内存管理器
type MemoryManager struct {
	maxMemoryMB int
	cache       map[string]*CacheEntry
	cacheMutex  sync.RWMutex
	running     bool
	stopCh      chan struct{}
	wg          sync.WaitGroup
	logger      logx.Logger
}

// CacheEntry 缓存条目
type CacheEntry struct {
	Data      interface{}
	CreatedAt time.Time
	TTL       time.Duration
	Size      int64
}

// NewMemoryManager 创建内存管理器
func NewMemoryManager(maxMemoryMB int) *MemoryManager {
	return &MemoryManager{
		maxMemoryMB: maxMemoryMB,
		cache:       make(map[string]*CacheEntry),
		stopCh:      make(chan struct{}),
		logger:      logx.WithContext(nil),
	}
}

// Start 启动内存管理器
func (mm *MemoryManager) Start() error {
	mm.running = true
	mm.logger.Infof("启动内存管理器，最大内存: %dMB", mm.maxMemoryMB)

	// 启动清理循环
	mm.wg.Add(1)
	go mm.cleanupLoop()

	return nil
}

// Stop 停止内存管理器
func (mm *MemoryManager) Stop() error {
	mm.running = false
	close(mm.stopCh)
	mm.wg.Wait()

	mm.logger.Info("内存管理器已停止")
	return nil
}

// cleanupLoop 清理循环
func (mm *MemoryManager) cleanupLoop() {
	defer mm.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mm.cleanup()
		case <-mm.stopCh:
			return
		}
	}
}

// cleanup 清理过期缓存
func (mm *MemoryManager) cleanup() {
	mm.cacheMutex.Lock()
	defer mm.cacheMutex.Unlock()

	now := time.Now()
	toRemove := make([]string, 0)

	for key, entry := range mm.cache {
		if entry.TTL > 0 && now.Sub(entry.CreatedAt) > entry.TTL {
			toRemove = append(toRemove, key)
		}
	}

	for _, key := range toRemove {
		delete(mm.cache, key)
	}

	if len(toRemove) > 0 {
		mm.logger.Infof("清理了 %d 个过期缓存条目", len(toRemove))
	}
}

// ClearCache 清理所有缓存
func (mm *MemoryManager) ClearCache() {
	mm.cacheMutex.Lock()
	defer mm.cacheMutex.Unlock()

	count := len(mm.cache)
	mm.cache = make(map[string]*CacheEntry)

	mm.logger.Infof("清理了所有缓存，共 %d 个条目", count)
}

// GetMemoryStats 获取内存统计
func (mm *MemoryManager) GetMemoryStats() *MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	mm.cacheMutex.RLock()
	cacheSize := len(mm.cache)
	mm.cacheMutex.RUnlock()

	return &MemoryStats{
		AllocMB:      float64(m.Alloc) / 1024 / 1024,
		TotalAllocMB: float64(m.TotalAlloc) / 1024 / 1024,
		SysMB:        float64(m.Sys) / 1024 / 1024,
		NumGC:        m.NumGC,
		CacheSize:    cacheSize,
	}
}

// MemoryStats 内存统计信息
type MemoryStats struct {
	AllocMB      float64 `json:"alloc_mb"`
	TotalAllocMB float64 `json:"total_alloc_mb"`
	SysMB        float64 `json:"sys_mb"`
	NumGC        uint32  `json:"num_gc"`
	CacheSize    int     `json:"cache_size"`
}
