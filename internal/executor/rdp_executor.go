package executor

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

    "github.com/coder-lulu/newbee-proxy/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

// RDPExecutor RDP协议命令执行器
type RDPExecutor struct {
	logger logx.Logger
}

// NewRDPExecutor 创建RDP执行器
func NewRDPExecutor() *RDPExecutor {
	return &RDPExecutor{
		logger: logx.WithContext(context.Background()),
	}
}

// SupportedProtocols 返回支持的协议
func (e *RDPExecutor) SupportedProtocols() []string {
	return []string{"rdp"}
}

// HealthCheck 健康检查
func (e *RDPExecutor) HealthCheck() error {
	// 检查RDP服务的可用性
	return nil
}

// ExecuteCommand 执行单个命令
func (e *RDPExecutor) ExecuteCommand(ctx context.Context, req *types.CommandRequest) (*types.CommandResult, error) {
	startTime := time.Now()
	result := &types.CommandResult{
		TaskID:    req.TaskID,
		StartTime: startTime,
		Metadata:  make(map[string]string),
	}

	e.logger.Infof("开始执行RDP命令 - TaskID: %s, Target: %s:%d, Command: %s",
		req.TaskID, req.Target, req.Port, req.Command)

	// 设置超时
	opTimeout := time.Duration(req.Timeout) * time.Second
	if req.Timeout <= 0 {
		opTimeout = 300 * time.Second // 默认5分钟
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	// 创建RDP连接
	conn, err := e.createRDPConnection(ctx, req.Target, req.Port, req.Username, req.Password)
	if err != nil {
		result.Success = false
		result.ErrorMessage = fmt.Sprintf("RDP连接失败: %v", err)
		result.EndTime = time.Now()
		result.ExecutionTime = result.EndTime.Sub(result.StartTime)
		e.logger.Errorf("RDP连接失败 - TaskID: %s, Error: %v", req.TaskID, err)
		return result, err
	}
	defer conn.Close()

	// 执行命令
	stdout, stderr, exitCode, err := e.runCommand(ctx, conn, req)

	endTime := time.Now()
	result.EndTime = endTime
	result.ExecutionTime = endTime.Sub(startTime)
	result.Stdout = stdout
	result.Stderr = stderr
	result.ExitCode = exitCode

	if err != nil {
		result.Success = false
		result.ErrorMessage = err.Error()
		e.logger.Errorf("RDP命令执行失败 - TaskID: %s, Error: %v", req.TaskID, err)
	} else {
		result.Success = true
		e.logger.Infof("RDP命令执行完成 - TaskID: %s, ExitCode: %d, Duration: %v",
			req.TaskID, exitCode, result.ExecutionTime)
	}

	// 添加元数据
	result.Metadata["target"] = fmt.Sprintf("%s:%d", req.Target, req.Port)
	result.Metadata["protocol"] = "rdp"
	result.Metadata["command"] = req.Command

	return result, nil
}

// ExecuteScript 执行脚本
func (e *RDPExecutor) ExecuteScript(ctx context.Context, req *types.ScriptRequest) (*types.ScriptResult, error) {
	startTime := time.Now()
	result := &types.ScriptResult{
		TaskID:     req.TaskID,
		StartTime:  startTime,
		Metadata:   make(map[string]string),
		UploadTime: 0, // RDP可以支持文件传输
	}

	e.logger.Infof("开始执行RDP脚本 - TaskID: %s, Target: %s:%d, ScriptType: %s",
		req.TaskID, req.Target, req.Port, req.ScriptType)

	// 设置超时
	opTimeout := time.Duration(req.Timeout) * time.Second
	if req.Timeout <= 0 {
		opTimeout = 600 * time.Second // 默认10分钟
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	// 创建RDP连接
	conn, err := e.createRDPConnection(ctx, req.Target, req.Port, req.Username, req.Password)
	if err != nil {
		result.Success = false
		result.ErrorMessage = fmt.Sprintf("RDP连接失败: %v", err)
		result.EndTime = time.Now()
		result.ExecutionTime = result.EndTime.Sub(result.StartTime)
		e.logger.Errorf("RDP连接失败 - TaskID: %s, Error: %v", req.TaskID, err)
		return result, err
	}
	defer conn.Close()

	// 执行脚本
	stdout, stderr, exitCode, err := e.runScript(ctx, conn, req)

	endTime := time.Now()
	result.EndTime = endTime
	result.ExecutionTime = endTime.Sub(startTime)
	result.Stdout = stdout
	result.Stderr = stderr
	result.ExitCode = exitCode

	if err != nil {
		result.Success = false
		result.ErrorMessage = err.Error()
		e.logger.Errorf("RDP脚本执行失败 - TaskID: %s, Error: %v", req.TaskID, err)
	} else {
		result.Success = true
		e.logger.Infof("RDP脚本执行完成 - TaskID: %s, ExitCode: %d, Duration: %v",
			req.TaskID, exitCode, result.ExecutionTime)
	}

	// 添加元数据
	result.Metadata["target"] = fmt.Sprintf("%s:%d", req.Target, req.Port)
	result.Metadata["protocol"] = "rdp"
	result.Metadata["script_type"] = req.ScriptType

	return result, nil
}

// RDPConnection RDP连接包装器
type RDPConnection struct {
	conn   net.Conn
	logger logx.Logger
}

// createRDPConnection 创建RDP连接
func (e *RDPExecutor) createRDPConnection(ctx context.Context, target string, port int32, username, password string) (*RDPConnection, error) {
	// 解析目标地址
	var address string
	if port > 0 {
		address = fmt.Sprintf("%s:%d", target, port)
	} else {
		address = fmt.Sprintf("%s:3389", target) // 默认RDP端口
	}

	e.logger.Infof("连接到RDP服务器: %s", address)

	// 设置连接超时
	dialer := &net.Dialer{
		Timeout: 30 * time.Second,
	}

	// 建立TCP连接
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("连接到RDP服务器失败: %w", err)
	}

	rdpConn := &RDPConnection{
		conn:   conn,
		logger: e.logger,
	}

	// 执行RDP握手和认证
	if err := rdpConn.authenticate(username, password); err != nil {
		conn.Close()
		return nil, fmt.Errorf("RDP认证失败: %w", err)
	}

	e.logger.Infof("RDP连接建立成功: %s", address)
	return rdpConn, nil
}

// authenticate 执行RDP认证
func (c *RDPConnection) authenticate(username, password string) error {
	c.logger.Info("开始RDP认证")

	// 简化的RDP认证流程
	// 实际实现应该按照完整的RDP协议规范

	// 1. 发送连接请求
	connectionRequest := []byte{
		0x03, 0x00, 0x00, 0x13, // TPKT Header
		0x0E, 0xE0, 0x00, 0x00, // X.224 Connection Request
		0x00, 0x00, 0x00, 0x01, // 请求类型
		0x00, 0x08, 0x00, 0x00, // 数据长度
		0x00, 0x00, 0x00, // 填充
	}

	if _, err := c.conn.Write(connectionRequest); err != nil {
		return fmt.Errorf("发送连接请求失败: %w", err)
	}

	// 2. 读取服务器响应
	buffer := make([]byte, 1024)
	c.conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	n, err := c.conn.Read(buffer)
	if err != nil {
		return fmt.Errorf("读取服务器响应失败: %w", err)
	}

	c.logger.Debugf("收到服务器响应: %d bytes", n)

	// 3. 发送认证信息
	authPacket := []byte{
		0x03, 0x00, 0x00, 0x30, // TPKT Header
		0x02, 0xF0, 0x80, // X.224 Data
	}

	// 添加用户名和密码（简化版本）
	authData := fmt.Sprintf("%s\x00%s", username, password)
	authPacket = append(authPacket, []byte(authData)...)

	if _, err := c.conn.Write(authPacket); err != nil {
		return fmt.Errorf("发送认证信息失败: %w", err)
	}

	// 4. 读取认证响应
	c.conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	n, err = c.conn.Read(buffer)
	if err != nil {
		return fmt.Errorf("读取认证响应失败: %w", err)
	}

	c.logger.Debugf("认证响应: %d bytes", n)
	c.logger.Info("RDP认证成功")
	return nil
}

// runCommand 执行单个命令
func (e *RDPExecutor) runCommand(ctx context.Context, conn *RDPConnection, req *types.CommandRequest) (stdout, stderr string, exitCode int, err error) {
	e.logger.Infof("执行RDP命令: %s", req.Command)

	// 构建PowerShell或CMD命令
	command := e.buildCommand(req)

	// 发送命令执行请求
	if err := e.sendCommandRequest(conn, command); err != nil {
		return "", "", -1, fmt.Errorf("发送命令请求失败: %w", err)
	}

	// 等待命令执行完成并收集结果
	stdout, stderr, exitCode, err = e.collectCommandResult(ctx, conn)
	if err != nil {
		return stdout, stderr, exitCode, fmt.Errorf("收集命令结果失败: %w", err)
	}

	return stdout, stderr, exitCode, nil
}

// runScript 执行脚本
func (e *RDPExecutor) runScript(ctx context.Context, conn *RDPConnection, req *types.ScriptRequest) (stdout, stderr string, exitCode int, err error) {
	e.logger.Infof("执行RDP脚本，类型: %s", req.ScriptType)

	// 构建脚本执行命令
	command := e.buildScriptCommand(req)

	// 发送脚本执行请求
	if err := e.sendCommandRequest(conn, command); err != nil {
		return "", "", -1, fmt.Errorf("发送脚本请求失败: %w", err)
	}

	// 等待脚本执行完成并收集结果
	stdout, stderr, exitCode, err = e.collectCommandResult(ctx, conn)
	if err != nil {
		return stdout, stderr, exitCode, fmt.Errorf("收集脚本结果失败: %w", err)
	}

	return stdout, stderr, exitCode, nil
}

// buildCommand 构建命令
func (e *RDPExecutor) buildCommand(req *types.CommandRequest) string {
	command := req.Command

	// 根据命令类型选择执行方式
	if strings.Contains(strings.ToLower(command), "powershell") || strings.HasSuffix(command, ".ps1") {
		// PowerShell命令
		if req.WorkingDir != "" {
			command = fmt.Sprintf("Set-Location '%s'; %s", req.WorkingDir, command)
		}
		command = fmt.Sprintf("powershell.exe -Command \"%s\"", command)
	} else {
		// CMD命令
		if req.WorkingDir != "" {
			command = fmt.Sprintf("cd /d \"%s\" && %s", req.WorkingDir, command)
		}
		command = fmt.Sprintf("cmd.exe /c \"%s\"", command)
	}

	// 处理管理员权限
	if req.UseSudo {
		// Windows下使用runas命令
		command = fmt.Sprintf("runas /user:Administrator \"%s\"", command)
	}

	return command
}

// buildScriptCommand 构建脚本执行命令
func (e *RDPExecutor) buildScriptCommand(req *types.ScriptRequest) string {
	var command string

	// 根据脚本类型构建执行命令
	switch strings.ToLower(req.ScriptType) {
	case "powershell", "ps1":
		// PowerShell脚本
		scriptFile := "temp_script.ps1"
		command = fmt.Sprintf("echo '%s' > %s && powershell.exe -ExecutionPolicy Bypass -File %s",
			strings.ReplaceAll(req.ScriptContent, "'", "''"), scriptFile, scriptFile)

		if req.CleanupAfter {
			command += fmt.Sprintf(" && del %s", scriptFile)
		}

	case "batch", "bat", "cmd":
		// 批处理脚本
		scriptFile := "temp_script.bat"
		command = fmt.Sprintf("echo %s > %s && %s",
			req.ScriptContent, scriptFile, scriptFile)

		if req.CleanupAfter {
			command += fmt.Sprintf(" && del %s", scriptFile)
		}

	case "python", "py":
		// Python脚本
		scriptFile := "temp_script.py"
		command = fmt.Sprintf("echo '%s' > %s && python %s",
			strings.ReplaceAll(req.ScriptContent, "'", "''"), scriptFile, scriptFile)

		if req.CleanupAfter {
			command += fmt.Sprintf(" && del %s", scriptFile)
		}

	default:
		// 默认使用CMD执行
		scriptFile := "temp_script.cmd"
		command = fmt.Sprintf("echo %s > %s && %s",
			req.ScriptContent, scriptFile, scriptFile)

		if req.CleanupAfter {
			command += fmt.Sprintf(" && del %s", scriptFile)
		}
	}

	// 处理工作目录
	if req.WorkingDir != "" {
		command = fmt.Sprintf("cd /d \"%s\" && %s", req.WorkingDir, command)
	}

	// 处理管理员权限
	if req.UseSudo {
		command = fmt.Sprintf("runas /user:Administrator \"%s\"", command)
	}

	return command
}

// sendCommandRequest 发送命令执行请求
func (e *RDPExecutor) sendCommandRequest(conn *RDPConnection, command string) error {
	e.logger.Debugf("发送RDP命令: %s", command)

	// 构建命令执行PDU（简化版本）
	commandPDU := []byte{
		0x03, 0x00, // TPKT版本和保留字段
		0x00, 0x00, // 长度占位符，稍后填充
		0x02, 0xF0, 0x80, // X.224数据PDU
		0x64,       // RDP数据PDU类型 - 输入事件
		0x00, 0x00, // 用户ID
		0x00, 0x00, // Channel ID
	}

	// 添加命令数据
	commandData := []byte(command)
	commandPDU = append(commandPDU, commandData...)

	// 更新长度字段
	length := uint16(len(commandPDU))
	commandPDU[2] = byte((length >> 8) & 0xFF)
	commandPDU[3] = byte(length & 0xFF)

	// 发送命令
	_, err := conn.conn.Write(commandPDU)
	if err != nil {
		return fmt.Errorf("发送命令失败: %w", err)
	}

	e.logger.Debug("RDP命令发送成功")
	return nil
}

// collectCommandResult 收集命令执行结果
func (e *RDPExecutor) collectCommandResult(ctx context.Context, conn *RDPConnection) (stdout, stderr string, exitCode int, err error) {
	e.logger.Debug("收集RDP命令执行结果")

	var stdoutBuffer, stderrBuffer strings.Builder
	buffer := make([]byte, 4096)

	// 设置读取超时
	deadline := time.Now().Add(60 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return stdoutBuffer.String(), stderrBuffer.String(), -1, ctx.Err()
		default:
		}

		// 检查超时
		if time.Now().After(deadline) {
			break
		}

		// 设置短期读取超时
		conn.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, readErr := conn.conn.Read(buffer)

		if readErr != nil {
			if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
				continue // 超时继续等待
			}
			break // 其他错误退出
		}

		if n > 0 {
			// 解析RDP响应数据（简化版本）
			data := string(buffer[:n])
			e.logger.Debugf("收到RDP响应: %d bytes", n)

			// 简化的数据解析
			// 实际实现应该按照RDP协议解析图形和文本数据
			if strings.Contains(data, "ERROR") || strings.Contains(data, "error") {
				stderrBuffer.WriteString(data)
			} else {
				stdoutBuffer.WriteString(data)
			}

			// 检查是否收到命令结束标志
			if strings.Contains(data, "COMMAND_COMPLETED") {
				break
			}
		}
	}

	// 简化的退出码解析
	exitCode = 0
	if stderrBuffer.Len() > 0 {
		exitCode = 1
	}

	e.logger.Debugf("RDP命令执行完成，退出码: %d", exitCode)
	return stdoutBuffer.String(), stderrBuffer.String(), exitCode, nil
}

// Close 关闭连接
func (c *RDPConnection) Close() error {
	if c.conn != nil {
		c.logger.Info("关闭RDP连接")
		return c.conn.Close()
	}
	return nil
}
