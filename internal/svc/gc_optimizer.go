package svc

import (
	"runtime"
	"runtime/debug"

	"github.com/zeromicro/go-zero/core/logx"
)

// GCOptimizer GC优化器
type GCOptimizer struct {
	targetPercent int
	logger        logx.Logger
}

// NewGCOptimizer 创建GC优化器
func NewGCOptimizer(targetPercent int) *GCOptimizer {
	return &GCOptimizer{
		targetPercent: targetPercent,
		logger:        logx.WithContext(nil),
	}
}

// SetTargetPercent 设置GC目标百分比
func (gc *GCOptimizer) SetTargetPercent(percent int) {
	if percent != gc.targetPercent {
		gc.targetPercent = percent
		debug.SetGCPercent(percent)
		gc.logger.Infof("GC目标百分比已调整为: %d", percent)
	}
}

// ForceGC 强制执行GC
func (gc *GCOptimizer) ForceGC() {
	runtime.GC()
	gc.logger.Info("已执行强制GC")
}

// GetGCStats 获取GC统计信息
func (gc *GCOptimizer) GetGCStats() *GCStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return &GCStats{
		NumGC:         m.NumGC,
		PauseTotal:    m.PauseTotalNs,
		LastPause:     m.PauseNs[(m.NumGC+255)%256],
		TargetPercent: gc.targetPercent,
	}
}

// GCStats GC统计信息
type GCStats struct {
	NumGC         uint32 `json:"num_gc"`
	PauseTotal    uint64 `json:"pause_total_ns"`
	LastPause     uint64 `json:"last_pause_ns"`
	TargetPercent int    `json:"target_percent"`
}

// Profiler 性能分析器
type Profiler struct {
	enabled bool
	logger  logx.Logger
}

// NewProfiler 创建性能分析器
func NewProfiler() *Profiler {
	return &Profiler{
		logger: logx.WithContext(nil),
	}
}

// Start 启动性能分析器
func (p *Profiler) Start() error {
	p.enabled = true
	p.logger.Info("性能分析器已启动")
	return nil
}

// Stop 停止性能分析器
func (p *Profiler) Stop() error {
	p.enabled = false
	p.logger.Info("性能分析器已停止")
	return nil
}

// IsEnabled 检查是否启用
func (p *Profiler) IsEnabled() bool {
	return p.enabled
}
