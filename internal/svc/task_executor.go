package svc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"newbee-agent/internal/executor"
	"newbee-agent/internal/types"
	pb "newbee-agent/proto/agent"

	"github.com/zeromicro/go-zero/core/logx"
)

// EnhancedTaskExecutor 增强版任务执行器
type EnhancedTaskExecutor struct {
	svcCtx  *ServiceContext
	logger  logx.Logger
	started bool
	stopCh  chan struct{}
	taskCh  chan *pb.TaskAssignment
	wg      sync.WaitGroup

	// 并发控制
	maxConcurrentTasks int
	currentTasks       int
	taskSemaphore      chan struct{}

	// 命令执行器映射
	executors map[string]types.CommandExecutor

	// 任务状态跟踪(内存缓存)
	activeTasks map[string]*TaskInfo
	taskMutex   sync.RWMutex

	// 结果持久化
	resultStorePath string
	cleanupTicker   *time.Ticker

	// OPS通信
	resultQueue chan *pb.TaskResult
}

// TaskInfo 任务信息
type TaskInfo struct {
	TaskID     string
	TaskType   pb.TaskType
	Status     types.ExecutionStatus
	StartTime  time.Time
	UpdateTime time.Time
	Cancel     context.CancelFunc

	// 执行结果 - 只保留基本信息，详细结果存储到文件
	HasResult    bool   `json:"has_result"`
	ResultStatus string `json:"result_status,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// TaskResult 完整的任务结果(用于文件存储)
type TaskResult struct {
	TaskID        string            `json:"task_id"`
	TaskType      string            `json:"task_type"`
	Status        string            `json:"status"`
	StartTime     time.Time         `json:"start_time"`
	EndTime       time.Time         `json:"end_time"`
	ExecutionTime int64             `json:"execution_time_ms"`
	ResultData    string            `json:"result_data"`
	ErrorMessage  string            `json:"error_message"`
	Metadata      map[string]string `json:"metadata"`
}

// NewEnhancedTaskExecutor 创建增强版任务执行器
func NewEnhancedTaskExecutor(svcCtx *ServiceContext) *EnhancedTaskExecutor {
	// 创建结果存储目录
	resultStorePath := filepath.Join(".", "task_results")
	if err := os.MkdirAll(resultStorePath, 0755); err != nil {
		svcCtx.Logger.Errorf("创建结果存储目录失败: %v", err)
	}

	taskExecutor := &EnhancedTaskExecutor{
		svcCtx:             svcCtx,
		logger:             svcCtx.Logger,
		stopCh:             make(chan struct{}),
		taskCh:             make(chan *pb.TaskAssignment, 1000), // 增大任务队列缓冲区
		maxConcurrentTasks: 50,                                  // 最大并发任务数
		taskSemaphore:      make(chan struct{}, 50),             // 并发控制信号量
		executors:          make(map[string]types.CommandExecutor),
		activeTasks:        make(map[string]*TaskInfo),
		resultStorePath:    resultStorePath,
		resultQueue:        make(chan *pb.TaskResult, 500), // 结果发送队列
	}

	// 注册SSH执行器
	sshExecutor := executor.NewSSHExecutor()
	for _, protocol := range sshExecutor.SupportedProtocols() {
		taskExecutor.executors[protocol] = sshExecutor
	}

	// 注册Telnet执行器
	telnetExecutor := executor.NewTelnetExecutor()
	for _, protocol := range telnetExecutor.SupportedProtocols() {
		taskExecutor.executors[protocol] = telnetExecutor
	}

	// 注册RDP执行器
	rdpExecutor := executor.NewRDPExecutor()
	for _, protocol := range rdpExecutor.SupportedProtocols() {
		taskExecutor.executors[protocol] = rdpExecutor
	}

	return taskExecutor
}

// Start 启动任务执行器
func (te *EnhancedTaskExecutor) Start() {
	if te.started {
		return
	}

	te.logger.Info("Starting enhanced task executor with enterprise features...")
	te.started = true

	// 启动任务处理协程
	te.wg.Add(1)
	go te.taskProcessingLoop()

	// 启动结果发送协程
	te.wg.Add(1)
	go te.resultSendingLoop()

	// 启动定时清理协程
	te.cleanupTicker = time.NewTicker(10 * time.Minute) // 每10分钟清理一次
	te.wg.Add(1)
	go te.cleanupLoop()

	te.logger.Infof("Enhanced task executor started - MaxConcurrent: %d, ResultPath: %s",
		te.maxConcurrentTasks, te.resultStorePath)
}

// Stop 停止任务执行器
func (te *EnhancedTaskExecutor) Stop() {
	if !te.started {
		return
	}

	te.logger.Info("Stopping enhanced task executor...")
	te.started = false

	// 停止定时清理
	if te.cleanupTicker != nil {
		te.cleanupTicker.Stop()
	}

	close(te.stopCh)
	te.wg.Wait()

	// 取消所有活跃任务
	te.taskMutex.Lock()
	for _, taskInfo := range te.activeTasks {
		if taskInfo.Cancel != nil {
			taskInfo.Cancel()
		}
	}
	te.activeTasks = make(map[string]*TaskInfo)
	te.taskMutex.Unlock()

	te.logger.Info("Enhanced task executor stopped")
}

// SubmitTask 提交任务
func (te *EnhancedTaskExecutor) SubmitTask(task *pb.TaskAssignment) error {
	// 安全检查，防止空指针
	if te == nil {
		return fmt.Errorf("任务执行器为nil")
	}
	if task == nil {
		return fmt.Errorf("任务不能为nil")
	}
	if task.TaskId == "" {
		return fmt.Errorf("任务ID不能为空")
	}

	if !te.started {
		te.logger.Errorf("尝试提交任务到未启动的执行器: %s", task.TaskId)
		return fmt.Errorf("任务执行器未启动")
	}

	// 检查任务是否已存在
	te.taskMutex.RLock()
	if _, exists := te.activeTasks[task.TaskId]; exists {
		te.taskMutex.RUnlock()
		te.logger.Errorf("任务ID重复: %s", task.TaskId)
		return fmt.Errorf("任务ID %s 已存在", task.TaskId)
	}
	te.taskMutex.RUnlock()

	// 使用defer确保异常处理
	defer func() {
		if r := recover(); r != nil {
			te.logger.Errorf("提交任务 %s 时发生panic: %v", task.TaskId, r)
		}
	}()

	select {
	case te.taskCh <- task:
		te.logger.Infof("任务已提交 - TaskID: %s, Type: %s", task.TaskId, task.TaskType)
		return nil
	case <-time.After(5 * time.Second):
		te.logger.Errorf("任务队列已满，无法提交任务: %s", task.TaskId)
		return fmt.Errorf("任务队列已满，无法提交任务 %s", task.TaskId)
	}
}

// GetTaskStatus 获取任务状态
func (te *EnhancedTaskExecutor) GetTaskStatus(taskID string) (*TaskInfo, bool) {
	te.taskMutex.RLock()
	defer te.taskMutex.RUnlock()
	taskInfo, exists := te.activeTasks[taskID]
	return taskInfo, exists
}

// GetActiveTasks 获取所有活跃任务
func (te *EnhancedTaskExecutor) GetActiveTasks() map[string]*TaskInfo {
	te.taskMutex.RLock()
	defer te.taskMutex.RUnlock()

	result := make(map[string]*TaskInfo)
	for k, v := range te.activeTasks {
		result[k] = v
	}
	return result
}

// taskProcessingLoop 任务处理循环
func (te *EnhancedTaskExecutor) taskProcessingLoop() {
	defer te.wg.Done()

	for {
		select {
		case task := <-te.taskCh:
			// 获取并发控制信号量
			select {
			case te.taskSemaphore <- struct{}{}:
				// 处理新任务
				go te.processTaskWithConcurrencyControl(task)
			default:
				// 并发数已满，记录警告并稍后重试
				te.logger.Errorf("任务并发数已达上限(%d)，任务 %s 延迟处理", te.maxConcurrentTasks, task.TaskId)
				go func(delayedTask *pb.TaskAssignment) {
					time.Sleep(1 * time.Second)
					te.taskCh <- delayedTask // 重新入队
				}(task)
			}

		case <-te.stopCh:
			return
		}
	}
}

// processTaskWithConcurrencyControl 带并发控制的任务处理
func (te *EnhancedTaskExecutor) processTaskWithConcurrencyControl(task *pb.TaskAssignment) {
	defer func() {
		<-te.taskSemaphore // 释放并发控制信号量
	}()

	te.processTask(task)
}

// processTask 处理单个任务
func (te *EnhancedTaskExecutor) processTask(task *pb.TaskAssignment) {
	taskID := task.TaskId

	// 添加recover来捕获panic
	defer func() {
		if r := recover(); r != nil {
			te.logger.Errorf("任务处理发生panic - TaskID: %s, Panic:  %v", taskID, r)

			// 确保清理任务状态 - 使用原子操作
			te.taskMutex.Lock()
			if taskInfo, exists := te.activeTasks[taskID]; exists {
				taskInfo.Status = types.StatusFailed
				taskInfo.HasResult = true
				taskInfo.ResultStatus = string(pb.TaskStatus_TASK_FAILED)
				taskInfo.ErrorMessage = fmt.Sprintf("任务处理发生panic: %v", r)
				taskInfo.UpdateTime = time.Now()
			}
			if te.currentTasks > 0 {
				te.currentTasks--
			}
			te.taskMutex.Unlock()
		}
	}()

	te.logger.Infof("开始处理任务 - TaskID: %s, Type: %s, Target: %s",
		taskID, task.TaskType, task.Target)

	// 检查任务是否已经存在
	te.taskMutex.Lock()
	if _, exists := te.activeTasks[taskID]; exists {
		te.logger.Errorf("任务已存在 - TaskID: %s, 跳过重复处理", taskID)
		te.taskMutex.Unlock()
		return
	}
	te.taskMutex.Unlock()

	// 创建任务上下文 - 添加额外的超时保护
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // 确保context被取消

	taskTimeout := time.Duration(task.TimeoutSeconds) * time.Second
	if task.TimeoutSeconds > 0 {
		// 为任务处理添加额外的缓冲时间（比任务超时多30秒）
		processingTimeout := taskTimeout + 30*time.Second
		ctx, cancel = context.WithTimeout(ctx, processingTimeout)
	} else {
		// 如果没有设置超时，使用默认5分钟
		taskTimeout = 5 * time.Minute
		ctx, cancel = context.WithTimeout(ctx, taskTimeout+30*time.Second)
	}

	// 注册任务信息 - 原子操作
	taskInfo := &TaskInfo{
		TaskID:     taskID,
		TaskType:   task.TaskType,
		Status:     types.StatusRunning,
		StartTime:  time.Now(),
		UpdateTime: time.Now(),
		Cancel:     cancel,
		HasResult:  false,
	}

	te.taskMutex.Lock()
	// 再次检查任务是否已存在（双重检查锁定模式）
	if _, exists := te.activeTasks[taskID]; exists {
		te.logger.Errorf("任务重复注册 - TaskID: %s, 跳过处理", taskID)
		te.taskMutex.Unlock()
		return
	}
	te.activeTasks[taskID] = taskInfo
	te.currentTasks++
	currentTaskCount := te.currentTasks
	te.taskMutex.Unlock()

	te.logger.Infof("任务已注册 - TaskID: %s, 当前活跃任务数: %d, 超时: %v", taskID, currentTaskCount, taskTimeout)

	// 启动超时监控goroutine
	go func() {
		timeoutTimer := time.NewTimer(taskTimeout + 10*time.Second) // 比任务超时多10秒
		defer timeoutTimer.Stop()

		select {
		case <-timeoutTimer.C:
			te.logger.Errorf("任务超时清理 - TaskID: %s, 执行时间超过限制", taskID)

			// 强制设置任务状态
			te.taskMutex.Lock()
			if taskInfo, exists := te.activeTasks[taskID]; exists && !taskInfo.HasResult {
				te.logger.Errorf("强制设置超时任务状态 - TaskID: %s", taskID)
				taskInfo.Status = types.StatusTimeout
				taskInfo.HasResult = true
				taskInfo.ResultStatus = string(pb.TaskStatus_TASK_TIMEOUT)
				taskInfo.ErrorMessage = "任务执行超时，被强制清理"
				taskInfo.UpdateTime = time.Now()

				// 创建超时结果
				timeoutResult := &pb.TaskResult{
					TaskId:          taskID,
					CommandId:       task.CommandId,
					Status:          pb.TaskStatus_TASK_TIMEOUT,
					ErrorMessage:    "任务执行超时",
					ExecutionTimeMs: int64(taskTimeout.Milliseconds()),
				}

				// 保存超时结果
				te.saveTaskResult(task, timeoutResult)
			}
			te.taskMutex.Unlock()

		case <-ctx.Done():
			// 正常完成，不需要清理
		}
	}()

	// 执行任务
	var result *pb.TaskResult

	te.logger.Infof("开始执行任务类型 - TaskID: %s, Type: %s", taskID, task.TaskType)

	switch task.TaskType {
	case pb.TaskType_COMMAND_EXECUTE:
		te.logger.Infof("执行命令任务 - TaskID: %s", taskID)
		result = te.executeCommand(ctx, task)
	case pb.TaskType_SCRIPT_EXECUTE:
		te.logger.Infof("执行脚本任务 - TaskID: %s", taskID)
		result = te.executeScript(ctx, task)
	default:
		te.logger.Errorf("不支持的任务类型 - TaskID: %s, Type: %s", taskID, task.TaskType)
		result = &pb.TaskResult{
			TaskId:          taskID,
			CommandId:       task.CommandId,
			Status:          pb.TaskStatus_TASK_FAILED,
			ErrorMessage:    fmt.Sprintf("不支持的任务类型: %s", task.TaskType),
			ExecutionTimeMs: 0,
		}
	}

	te.logger.Infof("任务执行完成 - TaskID: %s, Status: %s", taskID, result.Status)

	// 更新任务状态 - 确保原子操作
	te.taskMutex.Lock()
	if taskInfo, exists := te.activeTasks[taskID]; exists {
		// 检查是否已经被超时清理设置过
		if taskInfo.HasResult {
			te.logger.Infof("任务已被超时清理 - TaskID: %s, 跳过状态更新", taskID)
		} else {
			te.logger.Infof("更新任务状态 - TaskID: %s, 从 %s 到 %s", taskID, taskInfo.Status, result.Status)

			switch result.Status {
			case pb.TaskStatus_TASK_COMPLETED:
				taskInfo.Status = types.StatusCompleted
			case pb.TaskStatus_TASK_FAILED:
				taskInfo.Status = types.StatusFailed
			case pb.TaskStatus_TASK_TIMEOUT:
				taskInfo.Status = types.StatusTimeout
			case pb.TaskStatus_TASK_CANCELLED:
				taskInfo.Status = types.StatusCancelled
			}
			taskInfo.UpdateTime = time.Now()
			taskInfo.HasResult = true
			taskInfo.ResultStatus = string(result.Status)
			taskInfo.ErrorMessage = result.ErrorMessage

			te.logger.Infof("任务状态已更新 - TaskID: %s, HasResult: %t, Status: %s",
				taskID, taskInfo.HasResult, taskInfo.Status)

			// 保存任务结果
			te.logger.Infof("开始保存任务结果 - TaskID: %s", taskID)
			te.saveTaskResult(task, result)
		}
	} else {
		te.logger.Errorf("任务信息丢失 - TaskID: %s", taskID)
	}
	te.currentTasks--
	te.taskMutex.Unlock()

	te.logger.Infof("开始发送结果到队列 - TaskID: %s", taskID)

	// 异步发送结果到OPS服务
	select {
	case te.resultQueue <- result:
		te.logger.Infof("结果已入队 - TaskID: %s", taskID)
	default:
		te.logger.Errorf("结果发送队列已满，任务 %s 结果可能丢失", taskID)
	}

	// 取消上下文
	cancel()

	te.logger.Infof("任务处理完全结束 - TaskID: %s", taskID)
}

// executeCommand 执行命令任务
func (te *EnhancedTaskExecutor) executeCommand(ctx context.Context, task *pb.TaskAssignment) *pb.TaskResult {
	startTime := time.Now()

	// 解析任务载荷
	var commandReq types.CommandRequest
	if err := json.Unmarshal([]byte(task.Payload), &commandReq); err != nil {
		return &pb.TaskResult{
			TaskId:          task.TaskId,
			CommandId:       task.CommandId,
			Status:          pb.TaskStatus_TASK_FAILED,
			ErrorMessage:    fmt.Sprintf("解析命令参数失败: %v", err),
			ExecutionTimeMs: 0,
		}
	}

	// 填充基础信息
	commandReq.TaskID = task.TaskId
	commandReq.Target = task.Target
	commandReq.Port = task.TargetPort
	commandReq.Username = task.Credentials.Username
	commandReq.Password = task.Credentials.Password
	commandReq.PrivateKey = task.Credentials.PrivateKey
	if commandReq.Timeout == 0 {
		commandReq.Timeout = task.TimeoutSeconds
	}

	// 确定协议
	protocol := "ssh" // 默认使用ssh
	if val, exists := task.Options["protocol"]; exists {
		protocol = val
	}
	commandReq.Protocol = protocol

	// 获取执行器
	executor, exists := te.executors[protocol]
	if !exists {
		return &pb.TaskResult{
			TaskId:          task.TaskId,
			CommandId:       task.CommandId,
			Status:          pb.TaskStatus_TASK_FAILED,
			ErrorMessage:    fmt.Sprintf("不支持的协议: %s", protocol),
			ExecutionTimeMs: 0,
		}
	}

	// 执行命令
	result, err := executor.ExecuteCommand(ctx, &commandReq)
	executionTime := time.Since(startTime)

	// 构建返回结果
	taskResult := &pb.TaskResult{
		TaskId:          task.TaskId,
		CommandId:       task.CommandId,
		ExecutionTimeMs: executionTime.Milliseconds(),
	}

	if err != nil {
		taskResult.Status = pb.TaskStatus_TASK_FAILED
		taskResult.ErrorMessage = err.Error()
	} else {
		if result.Success {
			taskResult.Status = pb.TaskStatus_TASK_COMPLETED
		} else {
			taskResult.Status = pb.TaskStatus_TASK_FAILED
		}

		// 构建结果数据
		resultData := map[string]interface{}{
			"stdout":         result.Stdout,
			"stderr":         result.Stderr,
			"exit_code":      result.ExitCode,
			"execution_time": result.ExecutionTime.Milliseconds(),
			"metadata":       result.Metadata,
		}

		if jsonData, jsonErr := json.Marshal(resultData); jsonErr == nil {
			taskResult.ResultData = string(jsonData)
		}

		if !result.Success {
			taskResult.ErrorMessage = result.ErrorMessage
		}
	}

	return taskResult
}

// executeScript 执行脚本任务
func (te *EnhancedTaskExecutor) executeScript(ctx context.Context, task *pb.TaskAssignment) *pb.TaskResult {
	startTime := time.Now()

	// 解析任务载荷
	var scriptReq types.ScriptRequest
	if err := json.Unmarshal([]byte(task.Payload), &scriptReq); err != nil {
		return &pb.TaskResult{
			TaskId:          task.TaskId,
			CommandId:       task.CommandId,
			Status:          pb.TaskStatus_TASK_FAILED,
			ErrorMessage:    fmt.Sprintf("解析脚本参数失败: %v", err),
			ExecutionTimeMs: 0,
		}
	}

	// 填充基础信息
	scriptReq.TaskID = task.TaskId
	scriptReq.Target = task.Target
	scriptReq.Port = task.TargetPort
	scriptReq.Username = task.Credentials.Username
	scriptReq.Password = task.Credentials.Password
	scriptReq.PrivateKey = task.Credentials.PrivateKey
	if scriptReq.Timeout == 0 {
		scriptReq.Timeout = task.TimeoutSeconds
	}

	// 确定协议
	protocol := "ssh" // 默认使用ssh
	if val, exists := task.Options["protocol"]; exists {
		protocol = val
	}
	scriptReq.Protocol = protocol

	// 获取执行器
	executor, exists := te.executors[protocol]
	if !exists {
		return &pb.TaskResult{
			TaskId:          task.TaskId,
			CommandId:       task.CommandId,
			Status:          pb.TaskStatus_TASK_FAILED,
			ErrorMessage:    fmt.Sprintf("不支持的协议: %s", protocol),
			ExecutionTimeMs: 0,
		}
	}

	// 执行脚本
	result, err := executor.ExecuteScript(ctx, &scriptReq)
	executionTime := time.Since(startTime)

	// 构建返回结果
	taskResult := &pb.TaskResult{
		TaskId:          task.TaskId,
		CommandId:       task.CommandId,
		ExecutionTimeMs: executionTime.Milliseconds(),
	}

	if err != nil {
		taskResult.Status = pb.TaskStatus_TASK_FAILED
		taskResult.ErrorMessage = err.Error()
	} else {
		if result.Success {
			taskResult.Status = pb.TaskStatus_TASK_COMPLETED
		} else {
			taskResult.Status = pb.TaskStatus_TASK_FAILED
		}

		// 构建结果数据
		resultData := map[string]interface{}{
			"stdout":           result.Stdout,
			"stderr":           result.Stderr,
			"exit_code":        result.ExitCode,
			"execution_time":   result.ExecutionTime.Milliseconds(),
			"upload_time":      result.UploadTime.Milliseconds(),
			"remote_file_path": result.RemoteFilePath,
			"metadata":         result.Metadata,
		}

		if jsonData, jsonErr := json.Marshal(resultData); jsonErr == nil {
			taskResult.ResultData = string(jsonData)
		}

		if !result.Success {
			taskResult.ErrorMessage = result.ErrorMessage
		}
	}

	return taskResult
}

// sendTaskResult 发送任务结果到OPS服务
func (te *EnhancedTaskExecutor) sendTaskResult(result *pb.TaskResult) {
	if te.svcCtx.OpsClient == nil {
		te.logger.Error("OPS客户端未初始化，结果将仅保存在本地")
		return
	}

	// 创建上下文和超时
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 尝试发送结果，带重试机制
	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// TODO: 等待OPS服务定义TaskResultReport接口后实现
		// 当前记录日志，表示尝试发送
		te.logger.Infof("尝试发送任务结果到OPS - TaskID: %s, Status: %s, Attempt: %d/%d",
			result.TaskId, result.Status, attempt, maxRetries)

		// 模拟发送过程 - 在真实环境中这里会调用OPS的gRPC接口
		select {
		case <-ctx.Done():
			te.logger.Errorf("发送任务结果超时 - TaskID: %s, Attempt: %d", result.TaskId, attempt)
			break
		default:
			// 模拟成功发送
			te.logger.Infof("任务结果已模拟发送到OPS - TaskID: %s, Status: %s",
				result.TaskId, result.Status)
			return
		}

		// 如果不是最后一次尝试，等待后重试
		if attempt < maxRetries {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}

	// 所有重试都失败，记录错误
	te.logger.Errorf("任务结果发送到OPS最终失败 - TaskID: %s, 结果已保存在本地文件",
		result.TaskId)
}

// resultSendingLoop 结果发送循环
func (te *EnhancedTaskExecutor) resultSendingLoop() {
	defer te.wg.Done()

	for {
		select {
		case result := <-te.resultQueue:
			// 发送结果到OPS服务
			te.sendTaskResult(result)

		case <-te.stopCh:
			return
		}
	}
}

// cleanupLoop 清理循环 - 统一的内存管理
func (te *EnhancedTaskExecutor) cleanupLoop() {
	defer te.wg.Done()

	for {
		select {
		case <-te.cleanupTicker.C:
			te.performMemoryCleanup()

		case <-te.stopCh:
			return
		}
	}
}

// performMemoryCleanup 执行内存清理
func (te *EnhancedTaskExecutor) performMemoryCleanup() {
	te.taskMutex.Lock()
	defer te.taskMutex.Unlock()

	now := time.Now()
	var removedCount int
	var totalCount = len(te.activeTasks)

	// 清理逻辑：
	// 1. 已完成且超过30分钟的任务
	// 2. 运行中但超过2小时的任务（异常情况）
	for taskID, taskInfo := range te.activeTasks {
		shouldRemove := false

		// 已完成的任务，30分钟后清理
		if taskInfo.HasResult && now.Sub(taskInfo.UpdateTime) > 30*time.Minute {
			shouldRemove = true
		}

		// 运行中的任务，2小时后强制清理（防止异常任务占用内存）
		if !taskInfo.HasResult && now.Sub(taskInfo.StartTime) > 2*time.Hour {
			shouldRemove = true
			te.logger.Errorf("强制清理长时间运行任务 - TaskID: %s, 运行时间: %v",
				taskID, now.Sub(taskInfo.StartTime))

			// 尝试取消任务
			if taskInfo.Cancel != nil {
				taskInfo.Cancel()
			}
		}

		if shouldRemove {
			delete(te.activeTasks, taskID)
			removedCount++
		}
	}

	if removedCount > 0 {
		te.logger.Infof("内存清理完成 - 清理任务数: %d, 剩余任务数: %d, 清理前总数: %d",
			removedCount, len(te.activeTasks), totalCount)
	}

	// 每小时输出内存统计信息
	if now.Minute() == 0 {
		te.logMemoryStats()
	}
}

// logMemoryStats 记录内存统计信息
func (te *EnhancedTaskExecutor) logMemoryStats() {
	te.taskMutex.RLock()
	defer te.taskMutex.RUnlock()

	var runningCount, completedCount, failedCount int
	var oldestTaskTime time.Time
	var newestTaskTime time.Time

	now := time.Now()
	for _, taskInfo := range te.activeTasks {
		switch taskInfo.Status {
		case types.StatusRunning:
			runningCount++
		case types.StatusCompleted:
			completedCount++
		case types.StatusFailed, types.StatusTimeout, types.StatusCancelled:
			failedCount++
		}

		if oldestTaskTime.IsZero() || taskInfo.StartTime.Before(oldestTaskTime) {
			oldestTaskTime = taskInfo.StartTime
		}
		if newestTaskTime.IsZero() || taskInfo.StartTime.After(newestTaskTime) {
			newestTaskTime = taskInfo.StartTime
		}
	}

	var oldestAge time.Duration
	if !oldestTaskTime.IsZero() {
		oldestAge = now.Sub(oldestTaskTime)
	}

	te.logger.Infof("内存统计 - 总任务数: %d, 运行中: %d, 已完成: %d, 失败: %d, 最老任务: %v",
		len(te.activeTasks), runningCount, completedCount, failedCount, oldestAge)
}

// saveTaskResult 保存任务结果到文件
func (te *EnhancedTaskExecutor) saveTaskResult(task *pb.TaskAssignment, result *pb.TaskResult) {
	taskResult := &TaskResult{
		TaskID:        result.TaskId,
		TaskType:      task.TaskType.String(),
		Status:        result.Status.String(),
		StartTime:     time.Now().Add(-time.Duration(result.ExecutionTimeMs) * time.Millisecond),
		EndTime:       time.Now(),
		ExecutionTime: result.ExecutionTimeMs,
		ResultData:    result.ResultData,
		ErrorMessage:  result.ErrorMessage,
		Metadata: map[string]string{
			"target":     task.Target,
			"username":   task.Credentials.Username,
			"command_id": task.CommandId,
		},
	}

	// 序列化为JSON
	jsonData, err := json.MarshalIndent(taskResult, "", "  ")
	if err != nil {
		te.logger.Errorf("序列化任务结果失败 - TaskID: %s, Error: %v", result.TaskId, err)
		return
	}

	// 按日期创建子目录
	dateDir := time.Now().Format("2006-01-02")
	dayPath := filepath.Join(te.resultStorePath, dateDir)
	if err := os.MkdirAll(dayPath, 0755); err != nil {
		te.logger.Errorf("创建日期目录失败 - Date: %s, Error: %v", dateDir, err)
		return
	}

	// 写入文件
	filename := fmt.Sprintf("%s.json", result.TaskId)
	filePath := filepath.Join(dayPath, filename)

	if err := os.WriteFile(filePath, jsonData, 0644); err != nil {
		te.logger.Errorf("保存任务结果失败 - TaskID: %s, Path: %s, Error: %v",
			result.TaskId, filePath, err)
	} else {
		te.logger.Infof("任务结果已保存 - TaskID: %s, Path: %s", result.TaskId, filePath)
	}
}
