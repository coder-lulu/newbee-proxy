package svc

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// PerformanceOptimizer 性能优化器
type PerformanceOptimizer struct {
	// 基础配置
	config *PerformanceConfig
	logger logx.Logger

	// 并发控制
	goroutinePool     *GoroutinePool
	connectionLimiter *ConnectionLimiter
	rateLimiter       *RateLimiter

	// 内存管理
	memoryManager *MemoryManager
	gcOptimizer   *GCOptimizer

	// 性能监控
	metrics  *PerformanceMetrics
	profiler *Profiler

	// 状态控制
	running int32
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// PerformanceConfig 性能配置
type PerformanceConfig struct {
	// 并发配置
	MaxGoroutines     int `yaml:"max_goroutines"`      // 最大协程数
	MaxConnections    int `yaml:"max_connections"`     // 最大连接数
	RequestsPerSecond int `yaml:"requests_per_second"` // 每秒请求数限制

	// 内存配置
	MaxMemoryMB         int           `yaml:"max_memory_mb"`         // 最大内存使用(MB)
	GCTargetPercent     int           `yaml:"gc_target_percent"`     // GC目标百分比
	MemoryCheckInterval time.Duration `yaml:"memory_check_interval"` // 内存检查间隔

	// 缓存配置
	CacheSize int           `yaml:"cache_size"` // 缓存大小
	CacheTTL  time.Duration `yaml:"cache_ttl"`  // 缓存TTL

	// 优化配置
	EnableProfiling  bool          `yaml:"enable_profiling"`  // 启用性能分析
	EnableMetrics    bool          `yaml:"enable_metrics"`    // 启用指标收集
	OptimizeInterval time.Duration `yaml:"optimize_interval"` // 优化间隔
}

// DefaultPerformanceConfig 默认性能配置
func DefaultPerformanceConfig() *PerformanceConfig {
	return &PerformanceConfig{
		MaxGoroutines:       runtime.NumCPU() * 100,
		MaxConnections:      1000,
		RequestsPerSecond:   1000,
		MaxMemoryMB:         2048,
		GCTargetPercent:     100,
		MemoryCheckInterval: 30 * time.Second,
		CacheSize:           10000,
		CacheTTL:            5 * time.Minute,
		EnableProfiling:     true,
		EnableMetrics:       true,
		OptimizeInterval:    1 * time.Minute,
	}
}

// NewPerformanceOptimizer 创建性能优化器
func NewPerformanceOptimizer(config *PerformanceConfig) *PerformanceOptimizer {
	if config == nil {
		config = DefaultPerformanceConfig()
	}

	po := &PerformanceOptimizer{
		config: config,
		logger: logx.WithContext(context.Background()),
		stopCh: make(chan struct{}),
	}

	// 初始化组件
	po.goroutinePool = NewGoroutinePool(config.MaxGoroutines)
	po.connectionLimiter = NewConnectionLimiter(config.MaxConnections)
	po.rateLimiter = NewRateLimiter(config.RequestsPerSecond)
	po.memoryManager = NewMemoryManager(config.MaxMemoryMB)
	po.gcOptimizer = NewGCOptimizer(config.GCTargetPercent)

	if config.EnableMetrics {
		po.metrics = NewPerformanceMetrics()
	}

	if config.EnableProfiling {
		po.profiler = NewProfiler()
	}

	return po
}

// Start 启动性能优化器
func (po *PerformanceOptimizer) Start() error {
	if !atomic.CompareAndSwapInt32(&po.running, 0, 1) {
		return nil // 已经启动
	}

	po.logger.Info("启动性能优化器...")

	// 启动各个组件
	if err := po.goroutinePool.Start(); err != nil {
		return err
	}

	if err := po.memoryManager.Start(); err != nil {
		return err
	}

	if po.profiler != nil {
		if err := po.profiler.Start(); err != nil {
			po.logger.Errorf("启动性能分析器失败: %v", err)
		}
	}

	// 启动优化循环
	po.wg.Add(1)
	go po.optimizationLoop()

	po.logger.Info("性能优化器启动成功")
	return nil
}

// Stop 停止性能优化器
func (po *PerformanceOptimizer) Stop() error {
	if !atomic.CompareAndSwapInt32(&po.running, 1, 0) {
		return nil // 已经停止
	}

	po.logger.Info("停止性能优化器...")

	close(po.stopCh)
	po.wg.Wait()

	// 停止各个组件
	if po.profiler != nil {
		po.profiler.Stop()
	}

	po.memoryManager.Stop()
	po.goroutinePool.Stop()

	po.logger.Info("性能优化器已停止")
	return nil
}

// optimizationLoop 优化循环
func (po *PerformanceOptimizer) optimizationLoop() {
	defer po.wg.Done()

	ticker := time.NewTicker(po.config.OptimizeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			po.performOptimization()
		case <-po.stopCh:
			return
		}
	}
}

// performOptimization 执行优化
func (po *PerformanceOptimizer) performOptimization() {
	defer func() {
		if r := recover(); r != nil {
			po.logger.Errorf("性能优化过程中发生panic: %v", r)
		}
	}()

	// 收集当前指标
	currentMetrics := po.collectCurrentMetrics()

	// 内存优化
	po.optimizeMemory(currentMetrics)

	// 协程池优化
	po.optimizeGoroutinePool(currentMetrics)

	// 连接优化
	po.optimizeConnections(currentMetrics)

	// GC优化
	po.optimizeGC(currentMetrics)

	po.logger.Debugf("性能优化完成 - 内存使用: %.1fMB, 协程数: %d, 连接数: %d",
		currentMetrics.MemoryUsageMB, currentMetrics.GoroutineCount, currentMetrics.ConnectionCount)
}

// collectCurrentMetrics 收集当前指标
func (po *PerformanceOptimizer) collectCurrentMetrics() *CurrentMetrics {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return &CurrentMetrics{
		MemoryUsageMB:   float64(m.Alloc) / 1024 / 1024,
		GoroutineCount:  runtime.NumGoroutine(),
		ConnectionCount: po.connectionLimiter.GetActiveCount(),
		GCCount:         m.NumGC,
		GCPauseNs:       m.PauseNs[(m.NumGC+255)%256],
		HeapObjects:     m.HeapObjects,
		StackInUse:      m.StackInuse,
		Timestamp:       time.Now(),
	}
}

// optimizeMemory 内存优化
func (po *PerformanceOptimizer) optimizeMemory(metrics *CurrentMetrics) {
	// 检查内存使用是否超过阈值
	if metrics.MemoryUsageMB > float64(po.config.MaxMemoryMB)*0.8 {
		po.logger.Infof("内存使用率较高(%.1fMB), 执行内存优化", metrics.MemoryUsageMB)

		// 强制GC
		runtime.GC()

		// 清理缓存
		po.memoryManager.ClearCache()

		// 记录优化后的内存使用
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		newUsage := float64(m.Alloc) / 1024 / 1024
		po.logger.Infof("内存优化完成: %.1fMB -> %.1fMB", metrics.MemoryUsageMB, newUsage)
	}
}

// optimizeGoroutinePool 协程池优化
func (po *PerformanceOptimizer) optimizeGoroutinePool(metrics *CurrentMetrics) {
	// 动态调整协程池大小
	targetSize := po.calculateOptimalGoroutinePoolSize(metrics)
	currentSize := po.goroutinePool.GetSize()

	if targetSize != currentSize {
		po.logger.Infof("调整协程池大小: %d -> %d", currentSize, targetSize)
		po.goroutinePool.Resize(targetSize)
	}
}

// calculateOptimalGoroutinePoolSize 计算最优协程池大小
func (po *PerformanceOptimizer) calculateOptimalGoroutinePoolSize(metrics *CurrentMetrics) int {
	// 基于CPU核心数和当前负载计算
	cpuCount := runtime.NumCPU()
	baseSize := cpuCount * 50

	// 根据当前协程数调整
	if metrics.GoroutineCount > po.config.MaxGoroutines {
		return baseSize // 减少到基础大小
	}

	// 根据连接数调整
	connectionRatio := float64(metrics.ConnectionCount) / float64(po.config.MaxConnections)
	if connectionRatio > 0.8 {
		return int(float64(baseSize) * 1.5) // 增加50%
	}

	return baseSize
}

// optimizeConnections 连接优化
func (po *PerformanceOptimizer) optimizeConnections(metrics *CurrentMetrics) {
	// 清理空闲连接
	cleanedCount := po.connectionLimiter.CleanupIdleConnections()
	if cleanedCount > 0 {
		po.logger.Infof("清理了 %d 个空闲连接", cleanedCount)
	}
}

// optimizeGC GC优化
func (po *PerformanceOptimizer) optimizeGC(metrics *CurrentMetrics) {
	// 根据内存使用情况调整GC目标
	memoryRatio := metrics.MemoryUsageMB / float64(po.config.MaxMemoryMB)

	var targetPercent int
	switch {
	case memoryRatio > 0.8:
		targetPercent = 50 // 更频繁的GC
	case memoryRatio > 0.6:
		targetPercent = 75
	default:
		targetPercent = 100 // 默认值
	}

	po.gcOptimizer.SetTargetPercent(targetPercent)
}

// GetMetrics 获取性能指标
func (po *PerformanceOptimizer) GetMetrics() *PerformanceMetrics {
	return po.metrics
}

// GetCurrentStatus 获取当前状态
func (po *PerformanceOptimizer) GetCurrentStatus() *PerformanceStatus {
	metrics := po.collectCurrentMetrics()

	return &PerformanceStatus{
		IsRunning:         atomic.LoadInt32(&po.running) == 1,
		MemoryUsageMB:     metrics.MemoryUsageMB,
		GoroutineCount:    metrics.GoroutineCount,
		ConnectionCount:   metrics.ConnectionCount,
		GoroutinePoolSize: po.goroutinePool.GetSize(),
		ActiveWorkers:     po.goroutinePool.GetActiveWorkers(),
		QueuedTasks:       po.goroutinePool.GetQueuedTasks(),
		LastOptimization:  time.Now(),
	}
}

// CurrentMetrics 当前指标
type CurrentMetrics struct {
	MemoryUsageMB   float64
	GoroutineCount  int
	ConnectionCount int
	GCCount         uint32
	GCPauseNs       uint64
	HeapObjects     uint64
	StackInUse      uint64
	Timestamp       time.Time
}

// PerformanceStatus 性能状态
type PerformanceStatus struct {
	IsRunning         bool      `json:"is_running"`
	MemoryUsageMB     float64   `json:"memory_usage_mb"`
	GoroutineCount    int       `json:"goroutine_count"`
	ConnectionCount   int       `json:"connection_count"`
	GoroutinePoolSize int       `json:"goroutine_pool_size"`
	ActiveWorkers     int       `json:"active_workers"`
	QueuedTasks       int       `json:"queued_tasks"`
	LastOptimization  time.Time `json:"last_optimization"`
}

// PerformanceMetrics 性能指标
type PerformanceMetrics struct {
	mutex sync.RWMutex

	// 计数器
	TotalRequests      int64 `json:"total_requests"`
	SuccessfulRequests int64 `json:"successful_requests"`
	FailedRequests     int64 `json:"failed_requests"`

	// 延迟统计
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	P95LatencyMs float64 `json:"p95_latency_ms"`
	P99LatencyMs float64 `json:"p99_latency_ms"`

	// 吞吐量
	RequestsPerSecond float64 `json:"requests_per_second"`

	// 资源使用
	PeakMemoryMB    float64 `json:"peak_memory_mb"`
	PeakGoroutines  int     `json:"peak_goroutines"`
	PeakConnections int     `json:"peak_connections"`
}

// NewPerformanceMetrics 创建性能指标
func NewPerformanceMetrics() *PerformanceMetrics {
	return &PerformanceMetrics{}
}

// RecordRequest 记录请求
func (pm *PerformanceMetrics) RecordRequest(latencyMs float64, success bool) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	atomic.AddInt64(&pm.TotalRequests, 1)
	if success {
		atomic.AddInt64(&pm.SuccessfulRequests, 1)
	} else {
		atomic.AddInt64(&pm.FailedRequests, 1)
	}

	// 更新延迟统计 (简化实现，实际应使用更精确的统计方法)
	pm.AvgLatencyMs = (pm.AvgLatencyMs + latencyMs) / 2
}

// UpdateResourceUsage 更新资源使用
func (pm *PerformanceMetrics) UpdateResourceUsage(memoryMB float64, goroutines, connections int) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if memoryMB > pm.PeakMemoryMB {
		pm.PeakMemoryMB = memoryMB
	}

	if goroutines > pm.PeakGoroutines {
		pm.PeakGoroutines = goroutines
	}

	if connections > pm.PeakConnections {
		pm.PeakConnections = connections
	}
}

// GetSnapshot 获取指标快照
func (pm *PerformanceMetrics) GetSnapshot() *PerformanceMetrics {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	return &PerformanceMetrics{
		TotalRequests:      atomic.LoadInt64(&pm.TotalRequests),
		SuccessfulRequests: atomic.LoadInt64(&pm.SuccessfulRequests),
		FailedRequests:     atomic.LoadInt64(&pm.FailedRequests),
		AvgLatencyMs:       pm.AvgLatencyMs,
		P95LatencyMs:       pm.P95LatencyMs,
		P99LatencyMs:       pm.P99LatencyMs,
		RequestsPerSecond:  pm.RequestsPerSecond,
		PeakMemoryMB:       pm.PeakMemoryMB,
		PeakGoroutines:     pm.PeakGoroutines,
		PeakConnections:    pm.PeakConnections,
	}
}
