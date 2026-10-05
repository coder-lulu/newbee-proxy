package metrics

import (
    "runtime"
    "sync"
    "time"

    "github.com/shirou/gopsutil/v3/cpu"
    "github.com/shirou/gopsutil/v3/disk"
    "github.com/shirou/gopsutil/v3/mem"
    "github.com/shirou/gopsutil/v3/net"
    "github.com/prometheus/client_golang/prometheus"
)

// SystemMetrics 系统指标收集器
type SystemMetrics struct {
	mu              sync.RWMutex
	lastNetIO       *net.IOCountersStat
	lastNetIOTime   time.Time
	cpuUsage        float64
	memoryUsage     float64
	diskUsage       float64
	networkInDelta  uint64
	networkOutDelta uint64
}

// NewSystemMetrics 创建系统指标收集器
func NewSystemMetrics() *SystemMetrics {
    return &SystemMetrics{
        lastNetIOTime: time.Now(),
    }
}

// Collect 收集系统指标
func (s *SystemMetrics) Collect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. CPU使用率
	if cpuPercent, err := cpu.Percent(time.Second, false); err == nil && len(cpuPercent) > 0 {
		s.cpuUsage = cpuPercent[0]
	}

	// 2. 内存使用率
	if vmem, err := mem.VirtualMemory(); err == nil {
		s.memoryUsage = vmem.UsedPercent
	}

	// 3. 磁盘使用率（根分区）
	if diskStat, err := disk.Usage("/"); err == nil {
		s.diskUsage = diskStat.UsedPercent
	}

	// 4. 网络IO增量
	if netIO, err := net.IOCounters(false); err == nil && len(netIO) > 0 {
		currentIO := &netIO[0]
		now := time.Now()

		if s.lastNetIO != nil {
			// 计算增量（字节/秒）
			duration := now.Sub(s.lastNetIOTime).Seconds()
			if duration > 0 {
				s.networkInDelta = uint64(float64(currentIO.BytesRecv-s.lastNetIO.BytesRecv) / duration)
				s.networkOutDelta = uint64(float64(currentIO.BytesSent-s.lastNetIO.BytesSent) / duration)
			}
		}

		s.lastNetIO = currentIO
		s.lastNetIOTime = now
	}

	return nil
}

// GetMetrics 获取当前指标（线程安全）
func (s *SystemMetrics) GetMetrics() Metrics {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return Metrics{
		CPUUsage:        s.cpuUsage,
		MemoryUsage:     s.memoryUsage,
		DiskUsage:       s.diskUsage,
		NetworkInDelta:  s.networkInDelta,
		NetworkOutDelta: s.networkOutDelta,
		GoRoutines:      runtime.NumGoroutine(),
	}
}

// Metrics 指标数据结构
type Metrics struct {
    CPUUsage        float64 // CPU使用率 (0-100)
    MemoryUsage     float64 // 内存使用率 (0-100)
    DiskUsage       float64 // 磁盘使用率 (0-100)
    NetworkInDelta  uint64  // 网络入流量增量 (字节/秒)
    NetworkOutDelta uint64  // 网络出流量增量 (字节/秒)
    GoRoutines      int     // Goroutine数量
}

// Prometheus runtime/system 指标
var (
    goGoroutinesGauge = prometheus.NewGauge(prometheus.GaugeOpts{
        Namespace: "worker", Subsystem: "runtime", Name: "goroutines", Help: "Number of goroutines",
    })
    goGCCountCounter = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "runtime", Name: "gc_count_total", Help: "Total number of GC cycles observed",
    })
)

func RegisterRuntimeMetrics() {
    _ = prometheus.Register(goGoroutinesGauge)
    _ = prometheus.Register(goGCCountCounter)
}

// UpdateRuntimeMetrics 刷新 runtime 指标（在定时采样处调用）
func UpdateRuntimeMetrics() {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    goGoroutinesGauge.Set(float64(runtime.NumGoroutine()))
    // 此处将累计 GC 次数作为 Counter 追加（单调递增）
    // 注意：prometheus 不支持直接设置 Counter 值，因此采用差值追加（简化：每次追加 1 当检测到增长时）
    // 实际可维护上次值避免重复，这里保持简单实现
    // 留作后续完善
}
