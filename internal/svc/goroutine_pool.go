package svc

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// GoroutinePool 协程池
type GoroutinePool struct {
	// 配置
	maxSize     int
	minSize     int
	idleTimeout time.Duration

	// 工作队列
	taskQueue   chan func()
	workerQueue chan *worker

	// 工作者管理
	workers     map[int]*worker
	workerID    int32
	activeCount int32
	totalCount  int32

	// 状态控制
	running bool
	mutex   sync.RWMutex
	stopCh  chan struct{}
	wg      sync.WaitGroup

	// 日志
	logger logx.Logger
}

// worker 工作者
type worker struct {
	id       int
	pool     *GoroutinePool
	taskCh   chan func()
	stopCh   chan struct{}
	lastUsed time.Time
	mutex    sync.Mutex
}

// 错误定义
var (
	ErrPoolStopped = fmt.Errorf("协程池已停止")
	ErrPoolFull    = fmt.Errorf("协程池已满")
)

// NewGoroutinePool 创建协程池
func NewGoroutinePool(maxSize int) *GoroutinePool {
	if maxSize <= 0 {
		maxSize = 100
	}

	return &GoroutinePool{
		maxSize:     maxSize,
		minSize:     maxSize / 10, // 最小为最大的10%
		idleTimeout: 5 * time.Minute,
		taskQueue:   make(chan func(), maxSize*2),
		workerQueue: make(chan *worker, maxSize),
		workers:     make(map[int]*worker),
		stopCh:      make(chan struct{}),
		logger:      logx.WithContext(context.Background()),
	}
}

// Start 启动协程池
func (gp *GoroutinePool) Start() error {
	gp.mutex.Lock()
	defer gp.mutex.Unlock()

	if gp.running {
		return nil
	}

	gp.running = true
	gp.logger.Infof("启动协程池，最大大小: %d", gp.maxSize)

	// 启动调度器
	gp.wg.Add(1)
	go gp.scheduler()

	// 启动清理器
	gp.wg.Add(1)
	go gp.cleaner()

	// 预创建最小数量的工作者
	for i := 0; i < gp.minSize; i++ {
		gp.createWorker()
	}

	return nil
}

// Stop 停止协程池
func (gp *GoroutinePool) Stop() error {
	gp.mutex.Lock()
	defer gp.mutex.Unlock()

	if !gp.running {
		return nil
	}

	gp.running = false
	gp.logger.Info("停止协程池...")

	close(gp.stopCh)
	gp.wg.Wait()

	// 停止所有工作者
	for _, w := range gp.workers {
		close(w.stopCh)
	}

	gp.logger.Info("协程池已停止")
	return nil
}

// Submit 提交任务
func (gp *GoroutinePool) Submit(task func()) error {
	if !gp.isRunning() {
		return ErrPoolStopped
	}

	select {
	case gp.taskQueue <- task:
		return nil
	default:
		return ErrPoolFull
	}
}

// scheduler 调度器
func (gp *GoroutinePool) scheduler() {
	defer gp.wg.Done()

	for {
		select {
		case task := <-gp.taskQueue:
			gp.dispatchTask(task)
		case <-gp.stopCh:
			return
		}
	}
}

// dispatchTask 分发任务
func (gp *GoroutinePool) dispatchTask(task func()) {
	// 尝试获取空闲工作者
	select {
	case worker := <-gp.workerQueue:
		worker.taskCh <- task
		return
	default:
		// 没有空闲工作者，尝试创建新的
		if gp.canCreateWorker() {
			worker := gp.createWorker()
			worker.taskCh <- task
			return
		}
	}

	// 阻塞等待工作者
	select {
	case worker := <-gp.workerQueue:
		worker.taskCh <- task
	case <-gp.stopCh:
		return
	}
}

// canCreateWorker 检查是否可以创建新工作者
func (gp *GoroutinePool) canCreateWorker() bool {
	return int(atomic.LoadInt32(&gp.totalCount)) < gp.maxSize
}

// createWorker 创建工作者
func (gp *GoroutinePool) createWorker() *worker {
	id := int(atomic.AddInt32(&gp.workerID, 1))

	w := &worker{
		id:       id,
		pool:     gp,
		taskCh:   make(chan func(), 1),
		stopCh:   make(chan struct{}),
		lastUsed: time.Now(),
	}

	gp.mutex.Lock()
	gp.workers[id] = w
	gp.mutex.Unlock()

	atomic.AddInt32(&gp.totalCount, 1)

	go w.run()
	return w
}

// cleaner 清理器
func (gp *GoroutinePool) cleaner() {
	defer gp.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			gp.cleanupIdleWorkers()
		case <-gp.stopCh:
			return
		}
	}
}

// cleanupIdleWorkers 清理空闲工作者
func (gp *GoroutinePool) cleanupIdleWorkers() {
	gp.mutex.Lock()
	defer gp.mutex.Unlock()

	now := time.Now()
	toRemove := make([]*worker, 0)

	for _, w := range gp.workers {
		w.mutex.Lock()
		if now.Sub(w.lastUsed) > gp.idleTimeout && len(gp.workers) > gp.minSize {
			toRemove = append(toRemove, w)
		}
		w.mutex.Unlock()
	}

	for _, w := range toRemove {
		delete(gp.workers, w.id)
		close(w.stopCh)
		atomic.AddInt32(&gp.totalCount, -1)
	}

	if len(toRemove) > 0 {
		gp.logger.Infof("清理了 %d 个空闲工作者", len(toRemove))
	}
}

// run 工作者运行循环
func (w *worker) run() {
	defer func() {
		if r := recover(); r != nil {
			w.pool.logger.Errorf("工作者 %d 发生panic: %v", w.id, r)
		}
		atomic.AddInt32(&w.pool.activeCount, -1)
	}()

	atomic.AddInt32(&w.pool.activeCount, 1)

	for {
		select {
		case task := <-w.taskCh:
			w.executeTask(task)
			w.returnToPool()
		case <-w.stopCh:
			return
		}
	}
}

// executeTask 执行任务
func (w *worker) executeTask(task func()) {
	defer func() {
		if r := recover(); r != nil {
			w.pool.logger.Errorf("任务执行发生panic: %v", r)
		}
	}()

	w.mutex.Lock()
	w.lastUsed = time.Now()
	w.mutex.Unlock()

	task()
}

// returnToPool 返回到池中
func (w *worker) returnToPool() {
	select {
	case w.pool.workerQueue <- w:
	default:
		// 池已满，工作者将被丢弃
	}
}

// GetSize 获取池大小
func (gp *GoroutinePool) GetSize() int {
	return int(atomic.LoadInt32(&gp.totalCount))
}

// GetActiveWorkers 获取活跃工作者数量
func (gp *GoroutinePool) GetActiveWorkers() int {
	return int(atomic.LoadInt32(&gp.activeCount))
}

// GetQueuedTasks 获取队列中的任务数量
func (gp *GoroutinePool) GetQueuedTasks() int {
	return len(gp.taskQueue)
}

// Resize 调整池大小
func (gp *GoroutinePool) Resize(newSize int) {
	if newSize <= 0 {
		return
	}

	gp.mutex.Lock()
	defer gp.mutex.Unlock()

	oldSize := gp.maxSize
	gp.maxSize = newSize
	gp.minSize = newSize / 10

	gp.logger.Infof("协程池大小调整: %d -> %d", oldSize, newSize)
}

// isRunning 检查是否运行中
func (gp *GoroutinePool) isRunning() bool {
	gp.mutex.RLock()
	defer gp.mutex.RUnlock()
	return gp.running
}
