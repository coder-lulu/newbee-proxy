package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"newbee-agent/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	defaultTelnetTimeout       = 60 * time.Second
	defaultTelnetScriptTimeout = 300 * time.Second
	connectTimeoutTelnet       = 15 * time.Second
	authTimeoutTelnet          = 20 * time.Second // Time to wait for login/password prompts and submit credentials
	defaultTelnetPort          = 23
	readLoopDelayTelnet        = 100 * time.Millisecond
	maxReadBufferTelnet        = 10 * 1024 * 1024 // Max buffer for accumulated output
	defaultPromptTimeout       = 5 * time.Second  // Time to wait for a specific prompt
)

var (
	// Regexes for prompt detection
	loginPromptRegexTelnet    = regexp.MustCompile(`(?i)(login|username)[:\s]*$`)
	passwordPromptRegexTelnet = regexp.MustCompile(`(?i)password[:\s]*$`)
	// 更精确的命令提示符正则表达式
	commandPromptRegexTelnet = regexp.MustCompile(`(?i)[\w.-]+[@#>$%]\s*$|root[@#>$%]\s*$|\]\s*[#>$%]\s*$`)
	simplePromptRegexTelnet  = regexp.MustCompile(`[#>$%]\s*$`) // A simpler fallback
	// 系统信息识别正则表达式（避免误判）
	systemInfoRegexTelnet = regexp.MustCompile(`(?i)(system\s+load|memory\s+usage|welcome\s+to|processes|uptime|users\s+online)`)
)

// TelnetExecutor Telnet协议命令执行器
type TelnetExecutor struct {
	logger logx.Logger
}

// NewTelnetExecutor 创建Telnet执行器
func NewTelnetExecutor() *TelnetExecutor {
	return &TelnetExecutor{
		logger: logx.WithContext(context.Background()),
	}
}

// SupportedProtocols 返回支持的协议
func (e *TelnetExecutor) SupportedProtocols() []string {
	return []string{"telnet"}
}

// HealthCheck 健康检查
func (e *TelnetExecutor) HealthCheck() error {
	return nil
}

// TelnetConnection Telnet连接结构 using standard net package
type TelnetConnection struct {
	conn                net.Conn // Use standard net.Conn
	logger              logx.Logger
	loginPromptRegex    *regexp.Regexp
	passwordPromptRegex *regexp.Regexp
	commandPromptRegex  *regexp.Regexp
	reader              *bufio.Reader
}

// ExecuteCommand 执行单个命令
func (e *TelnetExecutor) ExecuteCommand(ctx context.Context, req *types.CommandRequest) (*types.CommandResult, error) {
	startTime := time.Now()
	result := &types.CommandResult{
		TaskID:    req.TaskID,
		StartTime: startTime,
		Metadata:  make(map[string]string),
	}

	e.logger.Infof("开始执行Telnet命令 (net包实现) - TaskID: %s, Target: %s:%d, Command: %s",
		req.TaskID, req.Target, req.Port, req.Command)

	opTimeout := time.Duration(req.Timeout) * time.Second
	if req.Timeout <= 0 {
		opTimeout = defaultTelnetTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	telnetConn, err := e.createTelnetConnection(ctx, req.Target, req.Port, req.Username, req.Password)
	if err != nil {
		result.Success = false
		result.ErrorMessage = fmt.Sprintf("Telnet连接失败 (net包): %v", err)
		result.EndTime = time.Now()
		result.ExecutionTime = result.EndTime.Sub(result.StartTime)
		e.logger.Errorf("Telnet连接创建失败 for TaskID %s: %v", req.TaskID, err)
		return result, err
	}
	defer telnetConn.Close()
	e.logger.Infof("Telnet连接成功 for TaskID %s", req.TaskID)

	command := e.buildCommand(req)

	stdout, stderr, exitCode, err := e.runCommand(ctx, telnetConn, command)

	endTime := time.Now()
	result.EndTime = endTime
	result.ExecutionTime = endTime.Sub(startTime)
	result.Stdout = stdout
	result.Stderr = stderr // Telnet不能直接分离stderr
	result.ExitCode = exitCode

	if err != nil {
		result.Success = false
		result.ErrorMessage = err.Error()
		e.logger.Errorf("命令执行失败 (net包) - TaskID: %s, Error: %v", req.TaskID, err)
	} else {
		result.Success = true
		e.logger.Infof("命令执行完成 (net包) - TaskID: %s, ExitCode: %d, Duration: %v",
			req.TaskID, exitCode, result.ExecutionTime)
	}

	result.Metadata["target"] = fmt.Sprintf("%s:%d", req.Target, req.Port)
	result.Metadata["protocol"] = "telnet_net"
	result.Metadata["command"] = req.Command

	return result, nil
}

// ExecuteScript 执行脚本（Telnet协议限制，需要逐行发送）
func (e *TelnetExecutor) ExecuteScript(ctx context.Context, req *types.ScriptRequest) (*types.ScriptResult, error) {
	startTime := time.Now()
	result := &types.ScriptResult{
		TaskID:     req.TaskID,
		StartTime:  startTime,
		Metadata:   make(map[string]string),
		UploadTime: 0, // No file upload for Telnet scripts
	}

	e.logger.Infof("开始执行Telnet脚本 (net包实现) - TaskID: %s, Target: %s:%d, ScriptType: %s",
		req.TaskID, req.Target, req.Port, req.ScriptType)

	opTimeout := time.Duration(req.Timeout) * time.Second
	if req.Timeout <= 0 {
		opTimeout = defaultTelnetScriptTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	telnetConn, err := e.createTelnetConnection(ctx, req.Target, req.Port, req.Username, req.Password)
	if err != nil {
		result.Success = false
		result.ErrorMessage = fmt.Sprintf("Telnet连接失败 (net包): %v", err)
		result.EndTime = time.Now()
		result.ExecutionTime = result.EndTime.Sub(result.StartTime)
		e.logger.Errorf("Telnet连接创建失败 for script TaskID %s: %v", req.TaskID, err)
		return result, err
	}
	defer telnetConn.Close()
	e.logger.Infof("Telnet连接成功 for script TaskID %s", req.TaskID)

	stdout, stderr, exitCode, err := e.runScript(ctx, telnetConn, req.ScriptContent, req.ScriptType)

	endTime := time.Now()
	result.EndTime = endTime
	result.ExecutionTime = endTime.Sub(startTime)
	result.Stdout = stdout
	result.Stderr = stderr // Telnet不能直接分离stderr
	result.ExitCode = exitCode
	result.RemoteFilePath = "N/A (inline execution)"

	if err != nil {
		result.Success = false
		result.ErrorMessage = err.Error()
		e.logger.Errorf("脚本执行失败 (net包) - TaskID: %s, Error: %v", req.TaskID, err)
	} else {
		result.Success = true
		e.logger.Infof("脚本执行完成 (net包) - TaskID: %s, ExitCode: %d, Duration: %v",
			req.TaskID, exitCode, result.ExecutionTime)
	}

	result.Metadata["target"] = fmt.Sprintf("%s:%d", req.Target, req.Port)
	result.Metadata["protocol"] = "telnet_net"
	result.Metadata["script_type"] = req.ScriptType
	result.Metadata["execution_mode"] = "inline"

	return result, nil
}

// createTelnetConnection 使用标准net包创建Telnet连接并完成认证
func (e *TelnetExecutor) createTelnetConnection(ctx context.Context, host string, port int32, username, password string) (*TelnetConnection, error) {
	if port <= 0 {
		port = defaultTelnetPort
	}
	address := fmt.Sprintf("%s:%d", host, port)

	e.logger.Infof("net包: 开始连接到 %s (超时: %v)", address, connectTimeoutTelnet)

	// 使用DialTimeout创建连接，支持超时
	dialer := &net.Dialer{
		Timeout: connectTimeoutTelnet,
	}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		e.logger.Errorf("net包: 连接失败 - %v", err)
		return nil, fmt.Errorf("net.Dial 失败 for %s: %w", address, err)
	}
	e.logger.Infof("net包: TCP连接成功 to %s", address)

	telnetConn := &TelnetConnection{
		conn:                conn,
		logger:              e.logger,
		loginPromptRegex:    loginPromptRegexTelnet,
		passwordPromptRegex: passwordPromptRegexTelnet,
		commandPromptRegex:  commandPromptRegexTelnet,
		reader:              bufio.NewReader(conn),
	}

	// 认证过程
	authCtx, authCancel := context.WithTimeout(ctx, authTimeoutTelnet)
	defer authCancel()

	// 尝试读取初始提示或欢迎信息，包括telnet协议握手
	e.logger.Infof("net包: 等待初始提示符和协议握手 (超时: %v)", defaultPromptTimeout*2)

	// 给telnet协议握手更多时间
	initialOutput, err := telnetConn.readUntilPromptWithProtocol(authCtx, []*regexp.Regexp{
		telnetConn.loginPromptRegex,
		telnetConn.passwordPromptRegex,
		telnetConn.commandPromptRegex,
		simplePromptRegexTelnet,
	}, defaultPromptTimeout*2)

	if err != nil {
		e.logger.Errorf("net包: 读取初始提示失败 - %v", err)
	}
	e.logger.Infof("net包: 收到初始输出 (长度: %d): %q", len(initialOutput), initialOutput)

	// 支持不同的认证模式
	if username != "" {
		// 用户名+密码认证模式
		e.logger.Infof("net包: 开始用户名+密码认证流程，用户名: %s", username)
		if err := e.performUsernamePasswordAuth(telnetConn, authCtx, initialOutput, username, password); err != nil {
			telnetConn.Close()
			return nil, err
		}
	} else if password != "" {
		// 仅密码认证模式
		e.logger.Infof("net包: 开始仅密码认证流程")
		if err := e.performPasswordOnlyAuth(telnetConn, authCtx, initialOutput, password); err != nil {
			telnetConn.Close()
			return nil, err
		}
	} else {
		// 无认证模式
		e.logger.Infof("net包: 无认证信息，检查是否已在命令提示符")
		if !e.isAtCommandPrompt(initialOutput) {
			telnetConn.Close()
			return nil, fmt.Errorf("设备需要认证但未提供用户名或密码")
		}
	}

	// 确保我们处于命令提示符状态，发送一个空回车确认
	e.logger.Infof("net包: 发送空回车确认命令提示符状态")
	if err := telnetConn.writeLine(""); err != nil {
		telnetConn.Close()
		return nil, fmt.Errorf("发送确认命令失败: %w", err)
	}

	// 等待最终的命令提示符
	e.logger.Infof("net包: 等待最终命令提示符确认 (超时: %v)", defaultPromptTimeout)
	finalOutput, err := telnetConn.readUntilPromptWithProtocol(authCtx, []*regexp.Regexp{
		telnetConn.commandPromptRegex,
		simplePromptRegexTelnet,
	}, defaultPromptTimeout)

	if err != nil {
		telnetConn.Close()
		e.logger.Errorf("net包: 等待最终命令提示符失败 - %v. 输出: %s", err, finalOutput)
		return nil, fmt.Errorf("等待最终命令提示符失败: %w", err)
	}

	e.logger.Infof("net包: 认证成功，确认命令提示符 for %s. 最终输出: %q", address, finalOutput)
	return telnetConn, nil
}

// performUsernamePasswordAuth 执行用户名+密码认证
func (e *TelnetExecutor) performUsernamePasswordAuth(conn *TelnetConnection, ctx context.Context, initialOutput, username, password string) error {
	// 检查是否在登录提示符
	if conn.loginPromptRegex.MatchString(initialOutput) || strings.Contains(strings.ToLower(initialOutput), "login") {
		e.logger.Infof("net包: 检测到登录提示符，发送用户名: %s", username)
		if err := conn.writeLine(username); err != nil {
			return fmt.Errorf("发送用户名失败: %w", err)
		}

		// 等待密码提示符
		e.logger.Infof("net包: 等待密码提示符 (超时: %v)", defaultPromptTimeout)
		_, err := conn.readUntilPromptWithProtocol(ctx, []*regexp.Regexp{conn.passwordPromptRegex}, defaultPromptTimeout)
		if err != nil {
			e.logger.Errorf("net包: 等待密码提示符失败 - %v", err)
			return fmt.Errorf("等待密码提示符失败: %w", err)
		}
		e.logger.Infof("net包: 收到密码提示符")

		e.logger.Infof("net包: 发送密码 (长度: %d)", len(password))
		if err := conn.writeLine(password); err != nil {
			return fmt.Errorf("发送密码失败: %w", err)
		}
	} else if conn.passwordPromptRegex.MatchString(initialOutput) || strings.Contains(strings.ToLower(initialOutput), "password") {
		// 直接是密码提示符，可能跳过了用户名
		e.logger.Infof("net包: 直接遇到密码提示符，发送密码")
		if err := conn.writeLine(password); err != nil {
			return fmt.Errorf("发送密码失败: %w", err)
		}
	} else if e.isAtCommandPrompt(initialOutput) {
		// 已经是命令提示符，可能是无密码登录
		e.logger.Infof("net包: 已经在命令提示符，无需认证")
		return nil
	} else {
		// 如果初始输出看起来像是欢迎信息，等待登录提示符
		e.logger.Infof("net包: 初始输出可能是欢迎信息，等待登录提示符")
		loginOutput, err := conn.readUntilPromptWithProtocol(ctx, []*regexp.Regexp{
			conn.loginPromptRegex,
			conn.passwordPromptRegex,
		}, defaultPromptTimeout)

		if err != nil {
			e.logger.Errorf("net包: 等待登录提示符失败 - %v", err)
			return fmt.Errorf("等待登录提示符失败: %w", err)
		}

		e.logger.Infof("net包: 收到登录相关提示符: %q", loginOutput)

		if conn.loginPromptRegex.MatchString(loginOutput) || strings.Contains(strings.ToLower(loginOutput), "login") {
			if err := conn.writeLine(username); err != nil {
				return fmt.Errorf("发送用户名失败: %w", err)
			}

			// 等待密码提示符
			_, err := conn.readUntilPromptWithProtocol(ctx, []*regexp.Regexp{conn.passwordPromptRegex}, defaultPromptTimeout)
			if err != nil {
				return fmt.Errorf("等待密码提示符失败: %w", err)
			}

			if err := conn.writeLine(password); err != nil {
				return fmt.Errorf("发送密码失败: %w", err)
			}
		} else if conn.passwordPromptRegex.MatchString(loginOutput) || strings.Contains(strings.ToLower(loginOutput), "password") {
			if err := conn.writeLine(password); err != nil {
				return fmt.Errorf("发送密码失败: %w", err)
			}
		}
	}
	return nil
}

// performPasswordOnlyAuth 执行仅密码认证
func (e *TelnetExecutor) performPasswordOnlyAuth(conn *TelnetConnection, ctx context.Context, initialOutput, password string) error {
	// 检查是否直接是密码提示符
	if conn.passwordPromptRegex.MatchString(initialOutput) || strings.Contains(strings.ToLower(initialOutput), "password") {
		e.logger.Infof("net包: 检测到密码提示符，发送密码")
		if err := conn.writeLine(password); err != nil {
			return fmt.Errorf("发送密码失败: %w", err)
		}
	} else if e.isAtCommandPrompt(initialOutput) {
		// 已经是命令提示符，无需认证
		e.logger.Infof("net包: 已经在命令提示符，无需密码认证")
		return nil
	} else {
		// 等待密码提示符
		e.logger.Infof("net包: 等待密码提示符出现")
		passwordOutput, err := conn.readUntilPromptWithProtocol(ctx, []*regexp.Regexp{
			conn.passwordPromptRegex,
		}, defaultPromptTimeout)

		if err != nil {
			e.logger.Errorf("net包: 等待密码提示符失败 - %v", err)
			return fmt.Errorf("等待密码提示符失败: %w", err)
		}

		e.logger.Infof("net包: 收到密码提示符: %q", passwordOutput)
		if err := conn.writeLine(password); err != nil {
			return fmt.Errorf("发送密码失败: %w", err)
		}
	}
	return nil
}

// isAtCommandPrompt 检查是否已在命令提示符
func (e *TelnetExecutor) isAtCommandPrompt(output string) bool {
	output = strings.TrimSpace(output)
	return simplePromptRegexTelnet.MatchString(output) ||
		strings.HasSuffix(output, "#") ||
		strings.HasSuffix(output, "$") ||
		strings.HasSuffix(output, ">")
}

// Close 关闭连接
func (tc *TelnetConnection) Close() error {
	if tc.conn != nil {
		tc.logger.Info("net包: 关闭连接")
		return tc.conn.Close()
	}
	return nil
}

// writeLine 发送命令行
func (tc *TelnetConnection) writeLine(cmd string) error {
	tc.logger.Debugf("net包: WRITE: %s", cmd)
	_, err := tc.conn.Write([]byte(cmd + "\r\n"))
	if err != nil {
		return fmt.Errorf("写入连接失败: %w", err)
	}
	return nil
}

// readUntilPromptWithProtocol 读取直到匹配某个提示符，同时处理telnet协议
func (tc *TelnetConnection) readUntilPromptWithProtocol(ctx context.Context, prompts []*regexp.Regexp, promptTimeout time.Duration) (string, error) {
	var buffer bytes.Buffer
	var cleanOutput bytes.Buffer // 清理后的输出，去除协议字符

	// 设置读取超时
	readDeadline := time.Now().Add(promptTimeout)
	tc.conn.SetReadDeadline(readDeadline)
	defer tc.conn.SetReadDeadline(time.Time{}) // 清除超时

	tc.logger.Infof("net包: 开始读取(协议处理)，等待提示符，超时: %v", promptTimeout)

	readCount := 0
	lastOutputLines := make([]string, 0) // 存储最近的几行输出用于分析

	for {
		select {
		case <-ctx.Done():
			tc.logger.Slowf("net包: readUntilPromptWithProtocol 被context取消: %v", ctx.Err())
			return cleanOutput.String(), fmt.Errorf("被context取消: %w", ctx.Err())
		default:
		}

		// 读取数据
		chunk := make([]byte, 1024)
		n, err := tc.reader.Read(chunk)
		if n > 0 {
			buffer.Write(chunk[:n])
			readCount++

			tc.logger.Infof("net包: 读取数据#%d (%d bytes): %q", readCount, n, string(chunk[:n]))

			// 处理telnet协议字符
			cleanData := tc.processTelnetProtocol(string(chunk[:n]))
			cleanOutput.WriteString(cleanData)
			currentCleanOutput := cleanOutput.String()

			tc.logger.Infof("net包: 协议处理后清理输出 (长度: %d): %q", len(currentCleanOutput), currentCleanOutput)

			// 更新最近的输出行
			lines := strings.Split(currentCleanOutput, "\n")
			if len(lines) > 5 {
				lastOutputLines = lines[len(lines)-5:] // 只保留最后5行
			} else {
				lastOutputLines = lines
			}

			// 检查最后一行是否为有效的提示符
			if len(lastOutputLines) > 0 {
				lastLine := strings.TrimSpace(lastOutputLines[len(lastOutputLines)-1])

				// 跳过明显的系统信息行
				if systemInfoRegexTelnet.MatchString(lastLine) {
					tc.logger.Debugf("net包: 跳过系统信息行: %q", lastLine)
					continue
				}

				// 检查是否匹配任何提示符 - 优先检查最后一行
				for i, promptRegex := range prompts {
					if promptRegex.MatchString(lastLine) {
						tc.logger.Infof("net包: 在最后一行找到提示符[%d] '%s': %q", i, promptRegex.String(), lastLine)
						return currentCleanOutput, nil
					}
				}

				// 简单的提示符字符检查 - 但要确保不是系统信息的一部分
				if tc.isValidCommandPrompt(lastLine) {
					tc.logger.Infof("net包: 通过字符串匹配找到有效提示符: %q", lastLine)
					return currentCleanOutput, nil
				}
			}

			// 检查缓冲区大小
			if buffer.Len() > maxReadBufferTelnet {
				return cleanOutput.String(), fmt.Errorf("读取缓冲区超过最大限制 (%d bytes)", maxReadBufferTelnet)
			}
		}

		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				tc.logger.Slowf("net包: 读取超时，当前清理缓冲区 (长度: %d): %q", cleanOutput.Len(), cleanOutput.String())
				return cleanOutput.String(), fmt.Errorf("读取超时: %w", err)
			}
			tc.logger.Errorf("net包: 读取错误: %v", err)
			return cleanOutput.String(), fmt.Errorf("读取错误: %w", err)
		}
	}
}

// isValidCommandPrompt 检查是否为有效的命令提示符（避免系统信息误判）
func (tc *TelnetConnection) isValidCommandPrompt(line string) bool {
	line = strings.TrimSpace(line)

	// 排除明显的系统信息
	if systemInfoRegexTelnet.MatchString(line) {
		return false
	}

	// 排除包含时间戳、数字统计等的行
	if strings.Contains(line, "CST") || strings.Contains(line, "GMT") ||
		strings.Contains(line, "uptime") || strings.Contains(line, "load") ||
		strings.Contains(line, "MB") || strings.Contains(line, "packages") {
		return false
	}

	// 简单的提示符字符检查
	if strings.HasSuffix(line, "#") || strings.HasSuffix(line, "$") ||
		strings.HasSuffix(line, ">") || strings.HasSuffix(line, "%") {
		// 确保行长度合理（提示符通常不会太长）
		if len(line) < 100 {
			return true
		}
	}

	return false
}

// processTelnetProtocol 处理telnet协议控制字符
func (tc *TelnetConnection) processTelnetProtocol(data string) string {
	// telnet协议字符处理
	var result []byte
	i := 0
	for i < len(data) {
		if i < len(data) && data[i] == '\xff' { // IAC (Interpret As Command)
			if i+1 < len(data) {
				cmd := data[i+1]
				switch cmd {
				case '\xfe': // DON'T
					if i+2 < len(data) {
						option := data[i+2]
						tc.logger.Debugf("net包: 收到DON'T命令，选项: %d", option)
						// 回应WON'T
						response := []byte{'\xff', '\xfc', option}
						tc.conn.Write(response)
						i += 3
					} else {
						i += 2
					}
				case '\xfd': // DO
					if i+2 < len(data) {
						option := data[i+2]
						tc.logger.Debugf("net包: 收到DO命令，选项: %d", option)
						// 回应WON'T (除非我们支持该选项)
						response := []byte{'\xff', '\xfc', option}
						tc.conn.Write(response)
						i += 3
					} else {
						i += 2
					}
				case '\xfc': // WON'T
					if i+2 < len(data) {
						option := data[i+2]
						tc.logger.Debugf("net包: 收到WON'T命令，选项: %d", option)
						i += 3
					} else {
						i += 2
					}
				case '\xfb': // WILL
					if i+2 < len(data) {
						option := data[i+2]
						tc.logger.Debugf("net包: 收到WILL命令，选项: %d", option)
						// 回应DON'T (除非我们需要该选项)
						response := []byte{'\xff', '\xfe', option}
						tc.conn.Write(response)
						i += 3
					} else {
						i += 2
					}
				case '\xf0': // SE (Subnegotiation End)
					tc.logger.Debugf("net包: 收到SE命令")
					i += 2
				case '\xfa': // SB (Subnegotiation Begin)
					tc.logger.Debugf("net包: 收到SB命令")
					// 跳过子协商，直到找到SE
					i += 2
					for i < len(data)-1 {
						if data[i] == '\xff' && data[i+1] == '\xf0' {
							i += 2
							break
						}
						i++
					}
				default:
					tc.logger.Debugf("net包: 跳过未知telnet命令: %d", cmd)
					i += 2
				}
			} else {
				i++
			}
		} else {
			// 普通字符，添加到结果
			result = append(result, data[i])
			i++
		}
	}

	return string(result)
}

// buildCommand 构建完整命令
func (e *TelnetExecutor) buildCommand(req *types.CommandRequest) string {
	command := req.Command
	if req.WorkingDir != "" {
		e.logger.Slowf("net包: WorkingDir (%s) for Telnet建议在命令中直接使用cd", req.WorkingDir)
	}
	if req.UseSudo {
		e.logger.Slowf("net包: UseSudo for Telnet需要在命令字符串中手动处理sudo")
	}
	return command
}

// runCommand 执行单个命令
func (e *TelnetExecutor) runCommand(ctx context.Context, conn *TelnetConnection, command string) (stdout, stderr string, exitCode int, err error) {
	e.logger.Infof("net包: 开始执行命令: %s", command)

	// 先发送一个简单的echo命令确认我们在正确的命令提示符
	e.logger.Infof("net包: 发送测试命令确认状态")
	testCommand := "echo 'READY'"
	if err := conn.writeLine(testCommand); err != nil {
		e.logger.Errorf("net包: 发送测试命令失败: %v", err)
		return "", "", -1, fmt.Errorf("发送测试命令失败: %w", err)
	}

	// 读取测试命令的响应
	expectedPrompts := []*regexp.Regexp{conn.commandPromptRegex, simplePromptRegexTelnet}
	testOutput, err := conn.readUntilPromptWithProtocol(ctx, expectedPrompts, defaultPromptTimeout)
	if err != nil {
		e.logger.Errorf("net包: 测试命令执行失败: %v", err)
		return "", "", -1, fmt.Errorf("测试命令执行失败: %w", err)
	}
	e.logger.Infof("net包: 测试命令输出: %q", testOutput)

	// 确认测试命令成功执行
	if !strings.Contains(testOutput, "READY") {
		e.logger.Errorf("net包: 测试命令未正确执行，可能不在命令提示符状态")
		return "", "", -1, fmt.Errorf("设备不在可执行命令状态")
	}

	// 现在执行真正的命令
	e.logger.Infof("net包: 设备状态确认正常，执行目标命令: %s", command)

	// 添加退出码标记
	exitCodeMarker := fmt.Sprintf("EXIT_CODE_MARKER_%d", time.Now().UnixNano())
	commandWithExitCode := fmt.Sprintf("%s; echo %s:$?", command, exitCodeMarker)

	e.logger.Infof("net包: 实际发送命令: %s", commandWithExitCode)
	if err := conn.writeLine(commandWithExitCode); err != nil {
		e.logger.Errorf("net包: 发送命令失败: %v", err)
		return "", "", -1, fmt.Errorf("发送命令失败: %w", err)
	}

	// 读取输出直到命令提示符
	commandTimeout := defaultPromptTimeout * 6 // 给命令更多时间
	e.logger.Infof("net包: 等待命令输出完成 (超时: %v)", commandTimeout)

	rawOutput, err := conn.readUntilPromptWithProtocol(ctx, expectedPrompts, commandTimeout)

	if err != nil {
		e.logger.Errorf("net包: 读取命令输出时出错 (命令: %s): %v. 收到的部分输出 (长度: %d): %q",
			command, err, len(rawOutput), rawOutput)
	} else {
		e.logger.Infof("net包: 成功读取命令输出. 原始输出长度: %d", len(rawOutput))
	}

	// 解析退出码
	stdout = rawOutput
	exitCode = 0 // 默认成功

	// 查找退出码标记
	markerIndex := strings.LastIndex(rawOutput, exitCodeMarker+":")
	if markerIndex != -1 {
		exitCodeStrPart := rawOutput[markerIndex+len(exitCodeMarker)+1:]

		// 找到退出码的结束位置
		endOfExitCode := 0
		for i, char := range exitCodeStrPart {
			if char < '0' || char > '9' {
				endOfExitCode = i
				break
			}
			endOfExitCode = i + 1
		}

		if endOfExitCode > 0 {
			exitCodeStr := strings.TrimSpace(exitCodeStrPart[:endOfExitCode])
			if parsedCode, parseErr := strconv.Atoi(exitCodeStr); parseErr == nil {
				exitCode = parsedCode
				e.logger.Infof("net包: 解析到退出码: %d (来自: %q)", exitCode, exitCodeStr)
				// 移除退出码标记行
				stdout = strings.TrimSpace(rawOutput[:markerIndex])
			} else {
				e.logger.Slowf("net包: 解析退出码失败: %v (尝试解析: %q)", parseErr, exitCodeStr)
			}
		}
	} else {
		e.logger.Slowf("net包: 未找到退出码标记 %s", exitCodeMarker)
	}

	// 清理输出 - 移除测试命令和其输出
	stdout = e.cleanOutput(stdout, commandWithExitCode, testCommand)

	e.logger.Infof("net包: 命令执行完成 - 退出码: %d, 清理后输出长度: %d", exitCode, len(stdout))
	if len(stdout) > 0 {
		e.logger.Infof("net包: 清理后输出内容: %q", stdout)
	}

	return stdout, "", exitCode, nil
}

// cleanOutput 清理命令输出
func (e *TelnetExecutor) cleanOutput(output, command string, testCommand ...string) string {
	lines := strings.Split(output, "\n")
	var cleanLines []string

	commandFirstLine := strings.Split(command, ";")[0]

	// 如果有测试命令，也要过滤掉
	var testCommandLines []string
	if len(testCommand) > 0 {
		for _, cmd := range testCommand {
			testCommandLines = append(testCommandLines, strings.Split(cmd, ";")[0])
		}
	}

	for _, line := range lines {
		trimmedLine := strings.TrimRight(line, "\r")

		// 跳过空行
		if strings.TrimSpace(trimmedLine) == "" {
			continue
		}

		// 跳过命令回显行
		if strings.Contains(trimmedLine, commandFirstLine) {
			e.logger.Debugf("net包: 跳过回显行: %s", trimmedLine)
			continue
		}

		// 跳过测试命令回显行
		shouldSkip := false
		for _, testCmd := range testCommandLines {
			if strings.Contains(trimmedLine, testCmd) || strings.Contains(trimmedLine, "READY") {
				e.logger.Debugf("net包: 跳过测试命令行: %s", trimmedLine)
				shouldSkip = true
				break
			}
		}
		if shouldSkip {
			continue
		}

		// 跳过提示符行
		if simplePromptRegexTelnet.MatchString(strings.TrimSpace(trimmedLine)) {
			e.logger.Debugf("net包: 跳过提示符行: %s", trimmedLine)
			continue
		}

		cleanLines = append(cleanLines, trimmedLine)
	}

	return strings.Join(cleanLines, "\n")
}

// runScript 执行脚本内容（逐行）
func (e *TelnetExecutor) runScript(ctx context.Context, conn *TelnetConnection, scriptContent, scriptType string) (stdout, stderr string, exitCode int, err error) {
	lines := strings.Split(strings.ReplaceAll(scriptContent, "\r\n", "\n"), "\n")
	var outputBuffer strings.Builder
	lastExitCode := 0

	var validCommands []string
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine != "" && !strings.HasPrefix(trimmedLine, "#") && !strings.HasPrefix(trimmedLine, "//") {
			validCommands = append(validCommands, trimmedLine)
		}
	}

	if len(validCommands) == 0 {
		e.logger.Info("net包: 脚本内容为空或只有注释")
		return "", "", 0, nil
	}

	e.logger.Infof("net包: 开始执行脚本，共 %d 条有效命令", len(validCommands))

	for i, lineCmd := range validCommands {
		// 检查context取消
		select {
		case <-ctx.Done():
			err = fmt.Errorf("脚本执行被context取消，在第 %d/%d 行 (%s): %w", i+1, len(validCommands), lineCmd, ctx.Err())
			e.logger.Errorf(err.Error())
			outputBuffer.WriteString(fmt.Sprintf("\nSCRIPT ABORTED: %s\n", err.Error()))
			return outputBuffer.String(), "", lastExitCode, err
		default:
		}

		e.logger.Infof("net包: 执行脚本行 %d/%d: %s", i+1, len(validCommands), lineCmd)

		lineStdout, _, lineExitCode, lineErr := e.runCommand(ctx, conn, lineCmd)

		outputBuffer.WriteString(lineStdout)
		if !strings.HasSuffix(lineStdout, "\n") && lineStdout != "" {
			outputBuffer.WriteString("\n")
		}

		if lineErr != nil {
			errMsg := fmt.Sprintf("脚本第 %d 行 '%s' 执行失败: %v", i+1, lineCmd, lineErr)
			e.logger.Errorf(errMsg)
			outputBuffer.WriteString(fmt.Sprintf("\nERROR: %s\n", errMsg))
			lastExitCode = -1
			return outputBuffer.String(), "", lastExitCode, fmt.Errorf(errMsg)
		}

		lastExitCode = lineExitCode
		if lineExitCode != 0 {
			e.logger.Slowf("net包: 脚本第 %d 行返回非零退出码: %d", i+1, lineExitCode)
		}

		progress := float64(i+1) / float64(len(validCommands)) * 100
		e.logger.Infof("net包: 脚本执行进度: %.1f%% (%d/%d)", progress, i+1, len(validCommands))
	}

	e.logger.Infof("net包: 脚本执行完成，最终退出码: %d", lastExitCode)
	return outputBuffer.String(), "", lastExitCode, nil
}
