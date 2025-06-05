package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"newbee-agent/internal/svc"
	"newbee-agent/internal/types"
	pb "newbee-agent/proto/agent"

	"github.com/zeromicro/go-zero/core/logx"
)

// TaskHandler 任务处理器
type TaskHandler struct {
	svcCtx *svc.ServiceContext
	logger logx.Logger
}

// NewTaskHandler 创建任务处理器
func NewTaskHandler(svcCtx *svc.ServiceContext) *TaskHandler {
	return &TaskHandler{
		svcCtx: svcCtx,
		logger: svcCtx.Logger,
	}
}

// CommandExecuteRequest 命令执行请求
type CommandExecuteRequest struct {
	Target       string            `json:"target"`        // 目标主机
	Port         int32             `json:"port"`          // 端口
	Protocol     string            `json:"protocol"`      // 协议 (ssh/telnet)
	Username     string            `json:"username"`      // 用户名
	Password     string            `json:"password"`      // 密码
	PrivateKey   string            `json:"private_key"`   // 私钥
	Command      string            `json:"command"`       // 命令
	WorkingDir   string            `json:"working_dir"`   // 工作目录
	Timeout      int32             `json:"timeout"`       // 超时时间(秒)
	UseSudo      bool              `json:"use_sudo"`      // 是否使用sudo
	SudoPassword string            `json:"sudo_password"` // sudo密码
	Environment  map[string]string `json:"environment"`   // 环境变量
}

// ScriptExecuteRequest 脚本执行请求
type ScriptExecuteRequest struct {
	Target         string            `json:"target"`           // 目标主机
	Port           int32             `json:"port"`             // 端口
	Protocol       string            `json:"protocol"`         // 协议 (ssh/telnet)
	Username       string            `json:"username"`         // 用户名
	Password       string            `json:"password"`         // 密码
	PrivateKey     string            `json:"private_key"`      // 私钥
	ScriptContent  string            `json:"script_content"`   // 脚本内容
	ScriptType     string            `json:"script_type"`      // 脚本类型
	WorkingDir     string            `json:"working_dir"`      // 工作目录
	Timeout        int32             `json:"timeout"`          // 超时时间(秒)
	UseSudo        bool              `json:"use_sudo"`         // 是否使用sudo
	SudoPassword   string            `json:"sudo_password"`    // sudo密码
	Environment    map[string]string `json:"environment"`      // 环境变量
	RemoteFileMode string            `json:"remote_file_mode"` // 文件权限
	RemoteFilePath string            `json:"remote_file_path"` // 远程文件路径
	CleanupAfter   bool              `json:"cleanup_after"`    // 执行后清理
}

// TaskResponse 任务响应
type TaskResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	TaskID  string `json:"task_id"`
}

// ExecuteCommand 执行命令
func (h *TaskHandler) ExecuteCommand(w http.ResponseWriter, r *http.Request) {
	var req CommandExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "请求参数解析失败: %v", err)
		return
	}

	// 验证必要参数
	if req.Target == "" || req.Username == "" || req.Command == "" {
		h.sendError(w, http.StatusBadRequest, "缺少必要参数: target, username, command")
		return
	}

	if req.Password == "" && req.PrivateKey == "" {
		h.sendError(w, http.StatusBadRequest, "必须提供password或private_key")
		return
	}

	// 生成任务ID
	taskID := fmt.Sprintf("cmd_%d", time.Now().UnixNano())

	// 确定协议，默认为ssh
	protocol := req.Protocol
	if protocol == "" {
		protocol = "ssh"
	}

	// 构建任务载荷
	commandReq := types.CommandRequest{
		TaskID:       taskID,
		Target:       req.Target,
		Port:         req.Port,
		Protocol:     protocol,
		Username:     req.Username,
		Password:     req.Password,
		PrivateKey:   req.PrivateKey,
		Command:      req.Command,
		WorkingDir:   req.WorkingDir,
		Timeout:      req.Timeout,
		UseSudo:      req.UseSudo,
		SudoPassword: req.SudoPassword,
		Environment:  req.Environment,
	}

	// 设置默认值
	if commandReq.Port == 0 {
		if protocol == "telnet" {
			commandReq.Port = 23 // telnet默认端口
		} else {
			commandReq.Port = 22 // ssh默认端口
		}
	}
	if commandReq.Timeout == 0 {
		commandReq.Timeout = 300 // 默认5分钟超时
	}

	// 转换为JSON载荷
	payloadBytes, err := json.Marshal(commandReq)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "构建任务载荷失败: %v", err)
		return
	}

	// 创建gRPC任务
	task := &pb.TaskAssignment{
		TaskId:     taskID,
		CommandId:  fmt.Sprintf("cmd_%d", time.Now().Unix()),
		TaskType:   pb.TaskType_COMMAND_EXECUTE,
		Target:     req.Target,
		TargetPort: req.Port,
		Credentials: &pb.Credentials{
			Username:   req.Username,
			Password:   req.Password,
			PrivateKey: req.PrivateKey,
			AuthMethod: "password",
		},
		Payload:        string(payloadBytes),
		TimeoutSeconds: req.Timeout,
		Priority:       3,
		Options:        map[string]string{"protocol": protocol},
	}

	// 提交任务
	if err := h.svcCtx.TaskExecutor.SubmitTask(task); err != nil {
		h.sendError(w, http.StatusInternalServerError, "提交任务失败: %v", err)
		return
	}

	h.logger.Infof("命令执行任务已提交 - TaskID: %s, Target: %s, Protocol: %s, Command: %s",
		taskID, req.Target, protocol, req.Command)

	h.sendResponse(w, TaskResponse{
		Success: true,
		Message: "命令执行任务已提交",
		TaskID:  taskID,
	})
}

// ExecuteScript 执行脚本
func (h *TaskHandler) ExecuteScript(w http.ResponseWriter, r *http.Request) {
	var req ScriptExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "请求参数解析失败: %v", err)
		return
	}

	// 验证必要参数
	if req.Target == "" || req.Username == "" || req.ScriptContent == "" {
		h.sendError(w, http.StatusBadRequest, "缺少必要参数: target, username, script_content")
		return
	}

	if req.Password == "" && req.PrivateKey == "" {
		h.sendError(w, http.StatusBadRequest, "必须提供password或private_key")
		return
	}

	// 生成任务ID
	taskID := fmt.Sprintf("script_%d", time.Now().UnixNano())

	// 确定协议，默认为ssh
	protocol := req.Protocol
	if protocol == "" {
		protocol = "ssh"
	}

	// 构建任务载荷
	scriptReq := types.ScriptRequest{
		TaskID:         taskID,
		Target:         req.Target,
		Port:           req.Port,
		Protocol:       protocol,
		Username:       req.Username,
		Password:       req.Password,
		PrivateKey:     req.PrivateKey,
		ScriptContent:  req.ScriptContent,
		ScriptType:     req.ScriptType,
		WorkingDir:     req.WorkingDir,
		Timeout:        req.Timeout,
		UseSudo:        req.UseSudo,
		SudoPassword:   req.SudoPassword,
		Environment:    req.Environment,
		RemoteFileMode: req.RemoteFileMode,
		RemoteFilePath: req.RemoteFilePath,
		CleanupAfter:   req.CleanupAfter,
	}

	// 设置默认值
	if scriptReq.Port == 0 {
		if protocol == "telnet" {
			scriptReq.Port = 23 // telnet默认端口
		} else {
			scriptReq.Port = 22 // ssh默认端口
		}
	}
	if scriptReq.Timeout == 0 {
		scriptReq.Timeout = 600 // 默认10分钟超时
	}
	if scriptReq.ScriptType == "" {
		scriptReq.ScriptType = "bash" // 默认bash脚本
	}
	if scriptReq.RemoteFileMode == "" {
		scriptReq.RemoteFileMode = "755" // 默认可执行权限
	}
	if !scriptReq.CleanupAfter {
		scriptReq.CleanupAfter = true // 默认执行后清理
	}

	// 转换为JSON载荷
	payloadBytes, err := json.Marshal(scriptReq)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "构建任务载荷失败: %v", err)
		return
	}

	// 创建gRPC任务
	task := &pb.TaskAssignment{
		TaskId:     taskID,
		CommandId:  fmt.Sprintf("script_%d", time.Now().Unix()),
		TaskType:   pb.TaskType_SCRIPT_EXECUTE,
		Target:     req.Target,
		TargetPort: req.Port,
		Credentials: &pb.Credentials{
			Username:   req.Username,
			Password:   req.Password,
			PrivateKey: req.PrivateKey,
			AuthMethod: "password",
		},
		Payload:        string(payloadBytes),
		TimeoutSeconds: req.Timeout,
		Priority:       3,
		Options:        map[string]string{"protocol": protocol},
	}

	// 提交任务
	if err := h.svcCtx.TaskExecutor.SubmitTask(task); err != nil {
		h.sendError(w, http.StatusInternalServerError, "提交任务失败: %v", err)
		return
	}

	h.logger.Infof("脚本执行任务已提交 - TaskID: %s, Target: %s, Protocol: %s, ScriptType: %s",
		taskID, req.Target, protocol, req.ScriptType)

	h.sendResponse(w, TaskResponse{
		Success: true,
		Message: "脚本执行任务已提交",
		TaskID:  taskID,
	})
}

// GetTaskStatus 获取任务状态
func (h *TaskHandler) GetTaskStatus(w http.ResponseWriter, r *http.Request) {
	// 从URL路径中提取taskId (go-zero正确方式)
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		h.sendError(w, http.StatusBadRequest, "缺少taskId参数")
		return
	}
	taskID := parts[4] // /api/task/status/:taskId

	if taskID == "" {
		h.sendError(w, http.StatusBadRequest, "缺少taskId参数")
		return
	}

	taskInfo, exists := h.svcCtx.TaskExecutor.GetTaskStatus(taskID)
	if !exists {
		h.sendError(w, http.StatusNotFound, "任务不存在: %s", taskID)
		return
	}

	response := map[string]interface{}{
		"task_id":     taskInfo.TaskID,
		"task_type":   taskInfo.TaskType.String(),
		"status":      string(taskInfo.Status),
		"start_time":  taskInfo.StartTime.Format(time.RFC3339),
		"update_time": taskInfo.UpdateTime.Format(time.RFC3339),
		"duration":    time.Since(taskInfo.StartTime).Milliseconds(),
	}

	h.sendResponse(w, response)
}

// GetActiveTasks 获取活跃任务列表
func (h *TaskHandler) GetActiveTasks(w http.ResponseWriter, r *http.Request) {
	activeTasks := h.svcCtx.TaskExecutor.GetActiveTasks()

	tasks := make([]map[string]interface{}, 0, len(activeTasks))
	for _, taskInfo := range activeTasks {
		tasks = append(tasks, map[string]interface{}{
			"task_id":     taskInfo.TaskID,
			"task_type":   taskInfo.TaskType.String(),
			"status":      string(taskInfo.Status),
			"start_time":  taskInfo.StartTime.Format(time.RFC3339),
			"update_time": taskInfo.UpdateTime.Format(time.RFC3339),
			"duration":    time.Since(taskInfo.StartTime).Milliseconds(),
		})
	}

	response := map[string]interface{}{
		"active_tasks": tasks,
		"total_count":  len(tasks),
	}

	h.sendResponse(w, response)
}

// GetTaskResult 获取任务执行结果
func (h *TaskHandler) GetTaskResult(w http.ResponseWriter, r *http.Request) {
	// 从URL路径中提取taskId
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		h.sendError(w, http.StatusBadRequest, "缺少taskId参数")
		return
	}
	taskID := parts[4] // /api/task/result/:taskId

	if taskID == "" {
		h.sendError(w, http.StatusBadRequest, "缺少taskId参数")
		return
	}

	taskInfo, exists := h.svcCtx.TaskExecutor.GetTaskStatus(taskID)
	if !exists {
		h.sendError(w, http.StatusNotFound, "任务不存在: %s", taskID)
		return
	}

	// 检查任务是否有结果
	if !taskInfo.HasResult {
		h.sendError(w, http.StatusNotFound, "任务还未完成或无执行结果")
		return
	}

	// 尝试从文件读取详细结果
	detailedResult, err := h.loadTaskResultFromFile(taskID)
	if err != nil {
		h.logger.Errorf("加载任务结果文件失败 - TaskID: %s, Error: %v", taskID, err)

		// 如果文件读取失败，返回基本信息
		response := map[string]interface{}{
			"task_id":       taskInfo.TaskID,
			"task_type":     taskInfo.TaskType.String(),
			"status":        string(taskInfo.Status),
			"start_time":    taskInfo.StartTime.Format(time.RFC3339),
			"update_time":   taskInfo.UpdateTime.Format(time.RFC3339),
			"duration":      time.Since(taskInfo.StartTime).Milliseconds(),
			"result_status": taskInfo.ResultStatus,
			"error_message": taskInfo.ErrorMessage,
			"has_result":    taskInfo.HasResult,
			"note":          "详细结果文件不可用，仅显示基本信息",
		}
		h.sendResponse(w, response)
		return
	}

	// 构建完整的结果响应
	response := map[string]interface{}{
		"task_id":       taskInfo.TaskID,
		"task_type":     taskInfo.TaskType.String(),
		"status":        string(taskInfo.Status),
		"start_time":    taskInfo.StartTime.Format(time.RFC3339),
		"update_time":   taskInfo.UpdateTime.Format(time.RFC3339),
		"duration":      time.Since(taskInfo.StartTime).Milliseconds(),
		"result_status": taskInfo.ResultStatus,
		"error_message": taskInfo.ErrorMessage,
		"has_result":    taskInfo.HasResult,

		// 详细结果信息
		"detailed_result": map[string]interface{}{
			"execution_time":  detailedResult.ExecutionTime,
			"result_data":     detailedResult.ResultData,
			"metadata":        detailedResult.Metadata,
			"file_start_time": detailedResult.StartTime.Format(time.RFC3339),
			"file_end_time":   detailedResult.EndTime.Format(time.RFC3339),
		},
	}

	// 如果有结果数据，尝试解析JSON
	if detailedResult.ResultData != "" {
		var resultData map[string]interface{}
		if err := json.Unmarshal([]byte(detailedResult.ResultData), &resultData); err == nil {
			response["parsed_result"] = resultData
		}
	}

	h.sendResponse(w, response)
}

// loadTaskResultFromFile 从文件加载任务结果
func (h *TaskHandler) loadTaskResultFromFile(taskID string) (*svc.TaskResult, error) {
	// 搜索最近几天的结果文件
	basePath := "task_results"

	// 检查今天和昨天的目录
	dates := []string{
		time.Now().Format("2006-01-02"),
		time.Now().AddDate(0, 0, -1).Format("2006-01-02"),
	}

	for _, date := range dates {
		filePath := fmt.Sprintf("%s/%s/%s.json", basePath, date, taskID)
		if data, err := os.ReadFile(filePath); err == nil {
			var result svc.TaskResult
			if err := json.Unmarshal(data, &result); err == nil {
				return &result, nil
			}
		}
	}

	return nil, fmt.Errorf("任务结果文件未找到")
}

// GetTaskStats 获取任务统计信息
func (h *TaskHandler) GetTaskStats(w http.ResponseWriter, r *http.Request) {
	activeTasks := h.svcCtx.TaskExecutor.GetActiveTasks()

	// 统计任务状态
	statusCount := make(map[string]int)
	typeCount := make(map[string]int)
	var totalDuration int64

	for _, taskInfo := range activeTasks {
		statusCount[string(taskInfo.Status)]++
		typeCount[taskInfo.TaskType.String()]++
		totalDuration += time.Since(taskInfo.StartTime).Milliseconds()
	}

	// 计算平均执行时间
	var avgDuration int64
	if len(activeTasks) > 0 {
		avgDuration = totalDuration / int64(len(activeTasks))
	}

	response := map[string]interface{}{
		"system_info": map[string]interface{}{
			"max_concurrent_tasks": 50, // 从配置中获取
			"active_task_count":    len(activeTasks),
			"avg_duration_ms":      avgDuration,
		},
		"status_breakdown": statusCount,
		"type_breakdown":   typeCount,
		"memory_usage": map[string]interface{}{
			"active_tasks_in_memory": len(activeTasks),
			"result_storage_type":    "file_based",
			"cleanup_interval":       "10 minutes",
		},
		"performance_metrics": map[string]interface{}{
			"total_processing_time_ms": totalDuration,
			"average_task_duration_ms": avgDuration,
		},
	}

	h.sendResponse(w, response)
}

// sendResponse 发送成功响应
func (h *TaskHandler) sendResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(data)
}

// sendError 发送错误响应
func (h *TaskHandler) sendError(w http.ResponseWriter, statusCode int, format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	h.logger.Error(message)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   message,
	})
}
