package svc

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "io"
    "sync"
    "time"

    "github.com/coder-lulu/newbee-proxy/internal/executor"
    metrics "github.com/coder-lulu/newbee-proxy/internal/metrics"
    "github.com/coder-lulu/newbee-proxy/internal/types"
    pb "github.com/coder-lulu/newbee-proxy/internal/types"
    httpplugin "github.com/coder-lulu/newbee-proxy/plugins/http"

    "github.com/zeromicro/go-zero/core/logx"
    sshlib "golang.org/x/crypto/ssh"
    "github.com/pkg/sftp"
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
    resultQueue chan *pb.PBTaskResult
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

    // 从配置读取任务队列与并发（提供合理默认值）
    queueSize := 1000
    maxConc := 50
    if svcCtx.Config.Limits.MaxConcurrentSessions > 0 {
        // 不直接绑定会话并发，这里保持独立，若未来提供专用字段再切换
    }
    if svcCtx.Config.Limits.TaskQueueSize > 0 {
        queueSize = svcCtx.Config.Limits.TaskQueueSize
    }
    if svcCtx.Config.Limits.MaxConcurrentTasks > 0 {
        maxConc = svcCtx.Config.Limits.MaxConcurrentTasks
    }

    taskExecutor := &EnhancedTaskExecutor{
        svcCtx:             svcCtx,
        logger:             svcCtx.Logger,
        stopCh:             make(chan struct{}),
        taskCh:             make(chan *pb.TaskAssignment, queueSize),
        maxConcurrentTasks: maxConc,
        taskSemaphore:      make(chan struct{}, maxConc),
        executors:          make(map[string]types.CommandExecutor),
        activeTasks:        make(map[string]*TaskInfo),
        resultStorePath:    resultStorePath,
        resultQueue:        make(chan *pb.PBTaskResult, 500), // 结果发送队列
        }

    // 指标：最大并发
    metrics.SetTaskMaxConcurrency(maxConc)

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
    // 停止后运行中任务数视为0
    metrics.SetTaskRunning(0)

	te.logger.Info("Enhanced task executor stopped")
}

// CancelTask 取消正在执行或排队中的任务
// 返回值：
//   - cancelled: 是否成功触发取消/标记取消
//   - err: 发生的错误（如任务不存在/已完成等）
func (te *EnhancedTaskExecutor) CancelTask(taskID string) (bool, error) {
    if te == nil {
        return false, fmt.Errorf("任务执行器未初始化")
    }

    te.taskMutex.Lock()
    defer te.taskMutex.Unlock()

    taskInfo, exists := te.activeTasks[taskID]
    if !exists {
        te.logger.Errorf("取消任务失败 - 任务不存在: %s", taskID)
        return false, fmt.Errorf("任务不存在: %s", taskID)
    }

    // 已经有结果说明已结束
    if taskInfo.HasResult || taskInfo.Status == types.StatusCompleted || taskInfo.Status == types.StatusFailed || taskInfo.Status == types.StatusTimeout || taskInfo.Status == types.StatusCancelled {
        te.logger.Infof("任务已结束或已被取消 - TaskID: %s, Status: %s", taskID, taskInfo.Status)
        return false, fmt.Errorf("任务已结束或已被取消")
    }

    // 触发Cancel（如果存在）
    if taskInfo.Cancel != nil {
        taskInfo.Cancel()
    }

    // 标记为已取消
    taskInfo.Status = types.StatusCancelled
    taskInfo.HasResult = true
    taskInfo.ResultStatus = string(pb.TaskStatus_TASK_CANCELLED)
    taskInfo.ErrorMessage = "任务被取消"
    taskInfo.UpdateTime = time.Now()

    if te.currentTasks > 0 {
        te.currentTasks--
    }

    te.logger.Infof("任务取消成功 - TaskID: %s", taskID)
    return true, nil
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

    metrics.IncTaskSubmitted()
    select {
    case te.taskCh <- task:
        te.logger.Infof("任务已提交 - TaskID: %s, Type: %s", task.TaskId, task.TaskType)
        metrics.SetTaskQueueLength(len(te.taskCh))
        return nil
    case <-time.After(5 * time.Second):
        metrics.IncTaskQueueRejects()
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

// GetRunningCount 获取当前运行中的任务数量（用于drain判定）
func (te *EnhancedTaskExecutor) GetRunningCount() int {
    te.taskMutex.RLock()
    defer te.taskMutex.RUnlock()
    return te.currentTasks
}

// taskProcessingLoop 任务处理循环
func (te *EnhancedTaskExecutor) taskProcessingLoop() {
	defer te.wg.Done()

	for {
		select {
        case task := <-te.taskCh:
            metrics.SetTaskQueueLength(len(te.taskCh))
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

    // 根据资源动态节流（示例：简单按队列长度控制）
    // 可扩展：结合内存/CPU 采样，计算 admission delay
    if len(te.taskCh) > cap(te.taskCh)/2 {
        metrics.SetTaskThrottleActive(true)
        delay := 200 * time.Millisecond
        metrics.AddTaskAdmitDelaySeconds(delay.Seconds())
        time.Sleep(delay)
    } else {
        metrics.SetTaskThrottleActive(false)
    }
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
            timeoutResult := &pb.PBTaskResult{
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

    // 执行任务（带简单重试）
    var result *pb.PBTaskResult
    maxRetries := parseIntOption(task.Options, "retries", 0)
    retryDelay := time.Duration(parseIntOption(task.Options, "retry_delay", 2)) * time.Second

    attempt := 0
    for {
        te.logger.Infof("开始执行任务类型 - TaskID: %s, Type: %s, Attempt: %d/%d", taskID, task.TaskType, attempt+1, maxRetries+1)
        switch task.TaskType {
        case pb.TaskType("COMMAND_EXECUTE"):
            result = te.executeCommand(ctx, task)
        case pb.TaskType("SCRIPT_EXECUTE"):
            result = te.executeScript(ctx, task)
        case pb.TaskType("FILE_TRANSFER"):
            result = te.executeFileTransfer(ctx, task)
        case pb.TaskType("http_request"):
            result = te.executeHTTP(ctx, task)
        default:
            te.logger.Errorf("不支持的任务类型 - TaskID: %s, Type: %s", taskID, task.TaskType)
            result = &pb.PBTaskResult{TaskId: taskID, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: fmt.Sprintf("不支持的任务类型: %s", task.TaskType)}
        }
        if result.Status == pb.TaskStatus_TASK_COMPLETED {
            break
        }
        if attempt >= maxRetries {
            break
        }
        attempt++
        metrics.AddTaskRetries(1)
        te.logger.Infof("任务失败准备重试 - TaskID: %s, 下一次在 %v 后", taskID, retryDelay)
        select {
        case <-ctx.Done():
            te.logger.Infof("任务上下文结束，停止重试 - TaskID: %s", taskID)
            break
        case <-time.After(retryDelay):
        }
    }

    te.logger.Infof("任务执行完成 - TaskID: %s, Status: %s", taskID, result.Status)
    metrics.ObserveTaskDurationSeconds(float64(result.ExecutionTimeMs) / 1000)

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
                metrics.IncTaskCompleted()
            case pb.TaskStatus_TASK_FAILED:
                taskInfo.Status = types.StatusFailed
                metrics.IncTaskFailed()
            case pb.TaskStatus_TASK_TIMEOUT:
                taskInfo.Status = types.StatusTimeout
                metrics.IncTaskTimeout()
            case pb.TaskStatus_TASK_CANCELLED:
                taskInfo.Status = types.StatusCancelled
                metrics.IncTaskCancelled()
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
    metrics.SetTaskRunning(te.currentTasks)
    te.taskMutex.Unlock()

	te.logger.Infof("开始发送结果到队列 - TaskID: %s", taskID)

	// 异步发送结果到OPS服务
	select {
    case te.resultQueue <- result:
        te.logger.Infof("结果已入队 - TaskID: %s", taskID)
        metrics.SetTaskResultQueueLength(len(te.resultQueue))
	default:
		te.logger.Errorf("结果发送队列已满，任务 %s 结果可能丢失", taskID)
	}

	// 取消上下文
	cancel()

	te.logger.Infof("任务处理完全结束 - TaskID: %s", taskID)
}

// parseIntOption 从任务Options解析整数
func parseIntOption(opts map[string]string, key string, def int) int {
    if opts == nil { return def }
    if v, ok := opts[key]; ok {
        if n, err := strconv.Atoi(v); err == nil {
            return n
        }
    }
    return def
}

// executeCommand 执行命令任务
func (te *EnhancedTaskExecutor) executeCommand(ctx context.Context, task *pb.TaskAssignment) *pb.PBTaskResult {
	startTime := time.Now()

	// 解析任务载荷
	var commandReq types.CommandRequest
	if err := json.Unmarshal([]byte(task.Payload), &commandReq); err != nil {
        return &pb.PBTaskResult{
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
        return &pb.PBTaskResult{
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
    taskResult := &pb.PBTaskResult{
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

// executeFileTransfer 执行文件传输（基于 SSH/SFTP）
func (te *EnhancedTaskExecutor) executeFileTransfer(ctx context.Context, task *pb.TaskAssignment) *pb.PBTaskResult {
    start := time.Now()
    var ftReq types.FileTransferRequest
    if err := json.Unmarshal([]byte(task.Payload), &ftReq); err != nil {
        return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: fmt.Sprintf("解析文件传输载荷失败: %v", err)}
    }
    // 默认值
    if ftReq.Protocol == "" { ftReq.Protocol = "ssh" }
    if ftReq.Port == 0 { ftReq.Port = 22 }
    if ftReq.Direction == "" { ftReq.Direction = "upload" }

    // 仅支持 ssh/sftp
    proto := strings.ToLower(ftReq.Protocol)
    if proto != "ssh" && proto != "sftp" {
        return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: fmt.Sprintf("不支持的文件传输协议: %s", ftReq.Protocol)}
    }

    // 创建 SSH 客户端（带超时）
    dialCtx, cancel := context.WithTimeout(ctx, time.Duration(ftReq.Timeout)*time.Second)
    defer cancel()

    sshConfig := &sshlib.ClientConfig{
        User:            ftReq.Username,
        HostKeyCallback: sshlib.InsecureIgnoreHostKey(),
        Timeout:         time.Duration(ftReq.Timeout) * time.Second,
    }
    if ftReq.Password != "" {
        sshConfig.Auth = append(sshConfig.Auth, sshlib.Password(ftReq.Password))
    }
    // TODO: 支持私钥认证

    address := fmt.Sprintf("%s:%d", ftReq.Target, ftReq.Port)
    var sshClient *sshlib.Client
    var err error
    done := make(chan struct{})
    go func() {
        sshClient, err = sshlib.Dial("tcp", address, sshConfig)
        close(done)
    }()
    select {
    case <-dialCtx.Done():
        return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_TIMEOUT, ErrorMessage: "连接目标超时"}
    case <-done:
        if err != nil {
            return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: fmt.Sprintf("建立SSH连接失败: %v", err)}
        }
    }
    defer sshClient.Close()

    sftpCli, err := sftp.NewClient(sshClient)
    if err != nil {
        return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: fmt.Sprintf("创建SFTP客户端失败: %v", err)}
    }
    defer sftpCli.Close()

    var bytesCopied int64
    transferDir := strings.ToLower(ftReq.Direction)
    switch strings.ToLower(ftReq.Direction) {
    case "upload":
        // 从本地 SourcePath 读取，写入远端 TargetPath
        lf, err := os.Open(ftReq.SourcePath)
        if err != nil {
            return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: fmt.Sprintf("打开本地文件失败: %v", err)}
        }
        defer lf.Close()
        rf, err := sftpCli.Create(ftReq.TargetPath)
        if err != nil {
            return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: fmt.Sprintf("创建远程文件失败: %v", err)}
        }
        defer rf.Close()
        bytesCopied, err = io.Copy(rf, lf)
        if err != nil {
            metrics.IncSFTPError("upload")
            return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: fmt.Sprintf("上传文件失败: %v", err)}
        }
    case "download":
        // 从远端 SourcePath 读取，写入本地 TargetPath
        rf, err := sftpCli.Open(ftReq.SourcePath)
        if err != nil {
        return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: fmt.Sprintf("打开远程文件失败: %v", err)}
        }
        defer rf.Close()
        lf, err := os.Create(ftReq.TargetPath)
        if err != nil {
        return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: fmt.Sprintf("创建本地文件失败: %v", err)}
        }
        defer lf.Close()
        bytesCopied, err = io.Copy(lf, rf)
        if err != nil {
            metrics.IncSFTPError("download")
            return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: fmt.Sprintf("下载文件失败: %v", err)}
        }
    default:
        return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: "不支持的Direction, 仅支持 upload/download"}
    }

    elapsed := time.Since(start)
    // 指标：字节数、耗时、吞吐
    if bytesCopied > 0 {
        metrics.AddSFTPBytes(transferDir, bytesCopied)
    }
    metrics.IncSFTPOps(transferDir)
    metrics.ObserveSFTPSeconds(transferDir, elapsed.Seconds())

    // 结果数据 JSON
    resultData := map[string]any{
        "direction": transferDir,
        "bytes_copied": bytesCopied,
        "transfer_duration_ms": elapsed.Milliseconds(),
    }
    if elapsed.Seconds() > 0 {
        resultData["throughput_bps"] = float64(bytesCopied) / elapsed.Seconds()
    }
    b, _ := json.Marshal(resultData)

    return &pb.PBTaskResult{
        TaskId:          task.TaskId,
        CommandId:       task.CommandId,
        Status:          pb.TaskStatus_TASK_COMPLETED,
        ExecutionTimeMs: int64(elapsed / time.Millisecond),
        ResultData:      string(b),
    }
}

// executeScript 执行脚本任务
func (te *EnhancedTaskExecutor) executeScript(ctx context.Context, task *pb.TaskAssignment) *pb.PBTaskResult {
	startTime := time.Now()

	// 解析任务载荷
	var scriptReq types.ScriptRequest
	if err := json.Unmarshal([]byte(task.Payload), &scriptReq); err != nil {
        return &pb.PBTaskResult{
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
        return &pb.PBTaskResult{
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
    taskResult := &pb.PBTaskResult{
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

// executeHTTP 执行 HTTP 请求任务
func (te *EnhancedTaskExecutor) executeHTTP(ctx context.Context, task *pb.TaskAssignment) *pb.PBTaskResult {
    start := time.Now()
    // 解析 payload 为 HTTP 请求
    var req httpplugin.HTTPRequest
    if err := json.Unmarshal([]byte(task.Payload), &req); err != nil {
        return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: fmt.Sprintf("解析HTTP请求失败: %v", err)}
    }

    // 获取插件
    plug, ok := te.svcCtx.PluginManager.GetPlugin("http")
    if !ok {
        return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: "HTTP插件未加载"}
    }
    hp, ok := plug.(*httpplugin.Plugin)
    if !ok {
        return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: "HTTP插件类型错误"}
    }

    // 执行
    resp, err := hp.DoRequest(ctx, &req)
    elapsed := time.Since(start)
    if err != nil {
        return &pb.PBTaskResult{TaskId: task.TaskId, CommandId: task.CommandId, Status: pb.TaskStatus_TASK_FAILED, ErrorMessage: err.Error(), ExecutionTimeMs: elapsed.Milliseconds()}
    }

    // 构造结果数据
    resultData := map[string]any{
        "status":        resp.Status,
        "size_bytes":    resp.SizeBytes,
        "duration_ms":   resp.DurationMs,
        "body_snippet":  resp.BodySnippet,
        "body_file_path": resp.BodyFilePath,
        "headers":       resp.Headers,
    }
    b, _ := json.Marshal(resultData)

    return &pb.PBTaskResult{
        TaskId:          task.TaskId,
        CommandId:       task.CommandId,
        Status:          pb.TaskStatus_TASK_COMPLETED,
        ExecutionTimeMs: elapsed.Milliseconds(),
        ResultData:      string(b),
    }
}

// sendTaskResult 发送任务结果到OPS服务 (Phase 2实现)
func (te *EnhancedTaskExecutor) sendTaskResult(result *pb.PBTaskResult) {

	// 构建任务结果payload
	statusStr := "unknown"
	switch result.Status {
	case pb.TaskStatus_TASK_COMPLETED:
		statusStr = "completed"
	case pb.TaskStatus_TASK_FAILED:
		statusStr = "failed"
	case pb.TaskStatus_TASK_TIMEOUT:
		statusStr = "timeout"
	case pb.TaskStatus_TASK_CANCELLED:
		statusStr = "cancelled"
	}

	payload := map[string]interface{}{
		"task_id":           result.TaskId,
		"status":            statusStr,
		"result_data":       result.ResultData,
		"error_message":     result.ErrorMessage,
		"execution_time_ms": result.ExecutionTimeMs,
	}

	// 尝试发送结果，带重试机制
	maxRetries := 3
    for attempt := 1; attempt <= maxRetries; attempt++ {
        te.logger.Infof("上报任务结果到OPS - TaskID: %s, Status: %s, Attempt: %d/%d",
            result.TaskId, statusStr, attempt, maxRetries)

        // 调用Ops-Center的任务结果上报接口（HTTP 客户端在 ProxyRegistrationManager 内）
        if te.svcCtx.ProxyReg == nil || te.svcCtx.ProxyReg.client == nil {
            te.logger.Errorf("OpsCenter client not ready, skip report")
            break
        }
        err := te.svcCtx.ProxyReg.client.ReportTaskResult(payload)
        if err == nil {
            te.logger.Infof("✅ 任务结果上报成功 - TaskID: %s, Status: %s",
                result.TaskId, statusStr)
            return
        }

		te.logger.Errorf("任务结果上报失败 - TaskID: %s, Attempt: %d, Error: %v",
			result.TaskId, attempt, err)

        // 如果不是最后一次尝试，等待后重试
        if attempt < maxRetries {
            time.Sleep(time.Duration(attempt) * time.Second)
        }
    }

    // 所有重试都失败，写入 outbox 等待异步重放
    te.logger.Errorf("❌ 任务结果上报最终失败 - TaskID: %s, 将写入 outbox", result.TaskId)
    if te.svcCtx.Store != nil {
        b, _ := json.Marshal(payload)
        type outboxWriter interface{ EnqueueOutbox(string, []byte) error }
        if w, ok := te.svcCtx.Store.(outboxWriter); ok {
            if err := w.EnqueueOutbox("task.result", b); err != nil {
                te.logger.Errorf("写入 outbox 失败 - TaskID: %s, Err: %v", result.TaskId, err)
            } else {
                te.logger.Infof("任务结果已写入 outbox - TaskID: %s", result.TaskId)
            }
        }
    }
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
func (te *EnhancedTaskExecutor) saveTaskResult(task *pb.TaskAssignment, result *pb.PBTaskResult) {
    taskResult := &TaskResult{
        TaskID:        result.TaskId,
        TaskType:      string(task.TaskType),
        Status:        string(result.Status),
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

    // 尝试写入本地 SQLite 元数据（可选）
    if te.svcCtx != nil && te.svcCtx.Store != nil {
        started := time.Now().Add(-time.Duration(result.ExecutionTimeMs) * time.Millisecond)
        finished := time.Now()
        target := task.Target
        protocol := "ssh"
        if v, ok := task.Options["protocol"]; ok { protocol = v }
        // 通过类型断言调用 InsertTaskMeta（避免引入具体类型到此文件）
        type taskMetaWriter interface{
            InsertTaskMeta(taskID, ttype, status, target, protocol string, startedAt, finishedAt time.Time, exitCode int, errMsg, resultPath string, bytes int64, tenantID, labels string) error
        }
        if w, ok := te.svcCtx.Store.(taskMetaWriter); ok {
            _ = w.InsertTaskMeta(
                result.TaskId,
                string(task.TaskType),
                string(result.Status),
                target,
                protocol,
                started,
                finished,
                0,
                result.ErrorMessage,
                filePath,
                int64(len(jsonData)),
                "", // tenantID 预留
                "", // labels 预留
            )
        }
    }
}
