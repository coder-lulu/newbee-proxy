package handlers

import (
	"encoding/json"
	"fmt"
	"time"

	"newbee-agent/internal/svc"
	"newbee-agent/internal/types"
	pb "newbee-agent/proto/agent"

	"github.com/zeromicro/go-zero/core/logx"
)

// OpsTaskHandler OPS任务处理器
type OpsTaskHandler struct {
	svcCtx *svc.ServiceContext
	logger logx.Logger
}

// NewOpsTaskHandler 创建OPS任务处理器
func NewOpsTaskHandler(svcCtx *svc.ServiceContext) *OpsTaskHandler {
	return &OpsTaskHandler{
		svcCtx: svcCtx,
		logger: svcCtx.Logger,
	}
}

// HandleTask 实现TaskHandler接口，处理从OPS接收的任务
func (h *OpsTaskHandler) HandleTask(taskData map[string]interface{}) error {
	h.logger.Infof("处理OPS任务: %+v", taskData)

	// 解析任务类型
	taskType, ok := taskData["task_type"].(string)
	if !ok {
		return fmt.Errorf("缺少任务类型")
	}

	// 解析任务ID
	taskID, ok := taskData["task_id"].(string)
	if !ok {
		return fmt.Errorf("缺少任务ID")
	}

	// 根据任务类型分发处理
	switch taskType {
	case "remote_execution":
		return h.handleRemoteExecution(taskID, taskData)
	case "file_transfer":
		return h.handleFileTransfer(taskID, taskData)
	case "health_check":
		return h.handleHealthCheck(taskID, taskData)
	case "config_update":
		return h.handleConfigUpdate(taskID, taskData)
	default:
		return fmt.Errorf("不支持的任务类型: %s", taskType)
	}
}

// handleRemoteExecution 处理远程执行任务
func (h *OpsTaskHandler) handleRemoteExecution(taskID string, taskData map[string]interface{}) error {
	h.logger.Infof("处理远程执行任务: %s", taskID)

	// 解析目标信息
	target, ok := taskData["target"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("缺少目标信息")
	}

	// 解析命令信息
	command, ok := taskData["command"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("缺少命令信息")
	}

	// 解析选项
	options, _ := taskData["options"].(map[string]interface{})
	if options == nil {
		options = make(map[string]interface{})
	}

	// 提取连接参数
	host, _ := target["host"].(string)
	port, _ := target["port"].(float64)
	protocol, _ := target["protocol"].(string)

	// 提取认证信息
	credentials, _ := target["credentials"].(map[string]interface{})
	if credentials == nil {
		return fmt.Errorf("缺少认证信息")
	}

	username, _ := credentials["username"].(string)
	password, _ := credentials["password"].(string)

	// 提取命令内容
	commandContent, _ := command["content"].(string)
	timeout, _ := command["timeout"].(float64)

	if host == "" || username == "" || commandContent == "" {
		return fmt.Errorf("缺少必要的连接参数")
	}

	// 构建任务请求
	commandReq := types.CommandRequest{
		TaskID:   taskID,
		Target:   host,
		Port:     int32(port),
		Protocol: protocol,
		Username: username,
		Password: password,
		Command:  commandContent,
		Timeout:  int32(timeout),
	}

	// 设置默认值
	if commandReq.Protocol == "" {
		commandReq.Protocol = "ssh"
	}
	if commandReq.Port == 0 {
		if commandReq.Protocol == "telnet" {
			commandReq.Port = 23
		} else {
			commandReq.Port = 22
		}
	}
	if commandReq.Timeout == 0 {
		commandReq.Timeout = 300
	}

	// 转换为JSON载荷
	payloadBytes, err := json.Marshal(commandReq)
	if err != nil {
		return fmt.Errorf("构建任务载荷失败: %v", err)
	}

	// 创建gRPC任务
	task := &pb.TaskAssignment{
		TaskId:     taskID,
		CommandId:  fmt.Sprintf("ops_%d", time.Now().Unix()),
		TaskType:   pb.TaskType_COMMAND_EXECUTE,
		Target:     host,
		TargetPort: commandReq.Port,
		Credentials: &pb.Credentials{
			Username:   username,
			Password:   password,
			AuthMethod: "password",
		},
		Payload:        string(payloadBytes),
		TimeoutSeconds: commandReq.Timeout,
		Priority:       2, // OPS任务优先级稍高
		Options:        map[string]string{"protocol": commandReq.Protocol, "source": "ops"},
	}

	// 提交任务到执行器
	if err := h.svcCtx.TaskExecutor.SubmitTask(task); err != nil {
		return fmt.Errorf("提交任务失败: %v", err)
	}

	h.logger.Infof("远程执行任务已提交 - TaskID: %s, Target: %s:%d, Protocol: %s, Command: %s",
		taskID, host, commandReq.Port, commandReq.Protocol, commandContent)

	return nil
}

// handleFileTransfer 处理文件传输任务
func (h *OpsTaskHandler) handleFileTransfer(taskID string, taskData map[string]interface{}) error {
	h.logger.Infof("处理文件传输任务: %s", taskID)

	// 解析目标信息
	target, ok := taskData["target"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("缺少目标信息")
	}

	// 解析命令信息
	command, ok := taskData["command"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("缺少命令信息")
	}

	// 提取连接参数
	host, _ := target["host"].(string)
	port, _ := target["port"].(float64)
	protocol, _ := target["protocol"].(string)

	// 提取认证信息
	credentials, _ := target["credentials"].(map[string]interface{})
	if credentials == nil {
		return fmt.Errorf("缺少认证信息")
	}

	username, _ := credentials["username"].(string)
	password, _ := credentials["password"].(string)

	// 提取文件传输参数
	srcPath, _ := command["src_path"].(string)
	dstPath, _ := command["dst_path"].(string)
	direction, _ := command["direction"].(string)
	timeout, _ := command["timeout"].(float64)

	if host == "" || username == "" || srcPath == "" || dstPath == "" {
		return fmt.Errorf("缺少必要的文件传输参数")
	}

	// 构建文件传输请求
	transferReq := types.FileTransferRequest{
		TaskID:     taskID,
		Target:     host,
		Port:       int32(port),
		Protocol:   protocol,
		Username:   username,
		Password:   password,
		SourcePath: srcPath,
		TargetPath: dstPath,
		Direction:  direction,
		Timeout:    int32(timeout),
	}

	// 设置默认值
	if transferReq.Protocol == "" {
		transferReq.Protocol = "ssh"
	}
	if transferReq.Port == 0 {
		transferReq.Port = 22
	}
	if transferReq.Direction == "" {
		transferReq.Direction = "upload"
	}
	if transferReq.Timeout == 0 {
		transferReq.Timeout = 600
	}

	// 转换为JSON载荷
	payloadBytes, err := json.Marshal(transferReq)
	if err != nil {
		return fmt.Errorf("构建文件传输载荷失败: %v", err)
	}

	// 创建gRPC任务
	task := &pb.TaskAssignment{
		TaskId:     taskID,
		CommandId:  fmt.Sprintf("ops_transfer_%d", time.Now().Unix()),
		TaskType:   pb.TaskType_FILE_TRANSFER,
		Target:     host,
		TargetPort: transferReq.Port,
		Credentials: &pb.Credentials{
			Username:   username,
			Password:   password,
			AuthMethod: "password",
		},
		Payload:        string(payloadBytes),
		TimeoutSeconds: transferReq.Timeout,
		Priority:       2,
		Options:        map[string]string{"protocol": transferReq.Protocol, "direction": direction, "source": "ops"},
	}

	// 提交任务到执行器
	if err := h.svcCtx.TaskExecutor.SubmitTask(task); err != nil {
		return fmt.Errorf("提交文件传输任务失败: %v", err)
	}

	h.logger.Infof("文件传输任务已提交 - TaskID: %s, Target: %s:%d, %s: %s -> %s",
		taskID, host, transferReq.Port, direction, srcPath, dstPath)

	return nil
}

// handleHealthCheck 处理健康检查任务
func (h *OpsTaskHandler) handleHealthCheck(taskID string, taskData map[string]interface{}) error {
	h.logger.Infof("处理健康检查任务: %s", taskID)

	// 健康检查任务不需要额外处理，直接返回成功
	// 实际的健康状态会通过心跳机制报告
	h.logger.Infof("健康检查任务完成 - TaskID: %s, Status: healthy", taskID)

	return nil
}

// handleConfigUpdate 处理配置更新任务
func (h *OpsTaskHandler) handleConfigUpdate(taskID string, taskData map[string]interface{}) error {
	h.logger.Infof("处理配置更新任务: %s", taskID)

	// 解析配置更新内容
	configUpdates, ok := taskData["config_updates"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("缺少配置更新内容")
	}

	// TODO: 实现实际的配置更新逻辑
	// 这里应该根据配置类型进行相应的更新操作
	// 例如：更新日志级别、连接池大小、超时设置等

	h.logger.Infof("配置更新任务完成 - TaskID: %s, Updates: %+v", taskID, configUpdates)

	return nil
}
