package executor

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "os"
    "path"
    "strconv"
    "strings"
    "time"

    "github.com/coder-lulu/newbee-proxy/internal/types"

    "github.com/pkg/sftp"
    "github.com/zeromicro/go-zero/core/logx"
    "golang.org/x/crypto/ssh"
)

// SSHExecutor SSH协议命令执行器
type SSHExecutor struct {
	logger logx.Logger
}

// NewSSHExecutor 创建SSH执行器
func NewSSHExecutor() *SSHExecutor {
	return &SSHExecutor{
		logger: logx.WithContext(context.Background()),
	}
}

// SupportedProtocols 返回支持的协议
func (e *SSHExecutor) SupportedProtocols() []string {
	return []string{"ssh"}
}

// HealthCheck 健康检查
func (e *SSHExecutor) HealthCheck() error {
	// 简单的健康检查，确保SSH库可用
	return nil
}

// ExecuteCommand 执行单个命令
func (e *SSHExecutor) ExecuteCommand(ctx context.Context, req *types.CommandRequest) (*types.CommandResult, error) {
	startTime := time.Now()
	result := &types.CommandResult{
		TaskID:    req.TaskID,
		StartTime: startTime,
		Metadata:  make(map[string]string),
	}

    // 日志脱敏：不打印具体命令内容
    e.logger.Infof("开始执行SSH命令 - TaskID: %s, Target: %s:%d",
        req.TaskID, req.Target, req.Port)

	// 设置超时
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
		defer cancel()
		e.logger.Infof("SSH命令执行设置超时: %d秒 - TaskID: %s", req.Timeout, req.TaskID)
	} else {
		e.logger.Infof("SSH命令执行未设置超时，使用默认值 - TaskID: %s", req.TaskID)
	}

	// 创建SSH客户端 - 使用context控制连接超时
	client, err := e.createSSHClientWithContext(ctx, req.Target, req.Port, req.Username, req.Password, req.PrivateKey)
	if err != nil {
		result.Success = false
		result.ErrorMessage = fmt.Sprintf("SSH连接失败: %v", err)
		result.EndTime = time.Now()
		result.ExecutionTime = result.EndTime.Sub(result.StartTime)
		e.logger.Errorf("SSH连接失败 - TaskID: %s, Error: %v", req.TaskID, err)
		return result, err
	}
	defer client.Close()

    // 创建SSH会话
    session, err := client.NewSession()
    if err != nil {
		result.Success = false
		result.ErrorMessage = fmt.Sprintf("创建SSH会话失败: %v", err)
		result.EndTime = time.Now()
		result.ExecutionTime = result.EndTime.Sub(result.StartTime)
		e.logger.Errorf("创建SSH会话失败 - TaskID: %s, Error: %v", req.TaskID, err)
		return result, err
	}
    defer session.Close()

    // 如果请求分配 PTY（解决 sudo requiretty），则申请 PTY
    if req.RequestPty {
        term := req.PtyTerm
        if term == "" { term = "xterm-256color" }
        rows := req.PtyRows; cols := req.PtyCols
        if rows <= 0 { rows = 40 }
        if cols <= 0 { cols = 120 }
        // ssh.TerminalModes 可选：关闭回显、设置速度
        modes := ssh.TerminalModes{
            ssh.ECHO:          1,    // 1 开回显，0 关
            ssh.TTY_OP_ISPEED: 14400,
            ssh.TTY_OP_OSPEED: 14400,
        }
        if err := session.RequestPty(term, rows, cols, modes); err != nil {
            // 不让失败中断执行，记录日志并继续无 PTY 模式
            e.logger.Errorf("RequestPty 失败，退回无 TTY 模式: %v", err)
        }
    }

    // 设置环境变量（优先 Setenv，失败将通过命令前缀 export 兜底）
    var exportPrefix string
    if req.Environment != nil {
        var failed bool
        for key, value := range req.Environment {
            if err := session.Setenv(key, value); err != nil {
                failed = true
            }
        }
        if failed {
            // 兜底：在命令前缀 export
            var b strings.Builder
            for k, v := range req.Environment {
                // 简单转义单引号
                v = strings.ReplaceAll(v, "'", "'\\''")
                fmt.Fprintf(&b, "export %s='%s'; ", k, v)
            }
            exportPrefix = b.String()
        }
    }

	// 构建完整命令
    command := e.buildCommand(req)
    if exportPrefix != "" {
        command = exportPrefix + command
    }
    // 标记 sudo 需求（用于 runCommand 内部探测与注入）
    if req.UseSudo {
        if req.SudoPreserveEnv {
            command = "__NB_SUDO_E__ " + command
        } else {
            command = "__NB_SUDO__ " + command
        }
        if req.SudoPassword != "" {
            // 将密码放入环境变量，供 runCommand 注入（避免明文出现在命令行）
            // 这里无法直接设置远端环境变量；我们采用 exportPrefix 兜底方式已经处理命令环境。
            // 对于 sudo 密码，使用本地进程环境传递（仅在当前 Run 期间），避免落盘。
            os.Setenv("NB_SUDO_PWD", req.SudoPassword)
            defer os.Unsetenv("NB_SUDO_PWD")
        }
    }

    // 执行命令（带 sudo/探测选项）
    stdout, stderr, exitCode, err := e.runCommand(ctx, session, command, req.UseSudo, req.SudoPreserveEnv, req.SudoPassword)

	endTime := time.Now()
	result.EndTime = endTime
	result.ExecutionTime = endTime.Sub(startTime)
	result.Stdout = stdout
	result.Stderr = stderr
	result.ExitCode = exitCode

	if err != nil {
		result.Success = false
		result.ErrorMessage = err.Error()
		e.logger.Errorf("命令执行失败 - TaskID: %s, Error: %v", req.TaskID, err)
	} else {
		result.Success = true
		e.logger.Infof("命令执行完成 - TaskID: %s, ExitCode: %d, Duration: %v",
			req.TaskID, exitCode, result.ExecutionTime)
	}

	// 添加元数据
	result.Metadata["target"] = fmt.Sprintf("%s:%d", req.Target, req.Port)
	result.Metadata["protocol"] = "ssh"
	result.Metadata["command"] = req.Command

	return result, nil
}

// ExecuteScript 执行脚本
func (e *SSHExecutor) ExecuteScript(ctx context.Context, req *types.ScriptRequest) (*types.ScriptResult, error) {
	startTime := time.Now()
	result := &types.ScriptResult{
		TaskID:    req.TaskID,
		StartTime: startTime,
		Metadata:  make(map[string]string),
	}

	e.logger.Infof("开始执行SSH脚本 - TaskID: %s, Target: %s:%d, ScriptType: %s",
		req.TaskID, req.Target, req.Port, req.ScriptType)

	// 设置超时
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
		defer cancel()
		e.logger.Infof("SSH脚本执行设置超时: %d秒 - TaskID: %s", req.Timeout, req.TaskID)
	} else {
		e.logger.Infof("SSH脚本执行未设置超时，使用默认值 - TaskID: %s", req.TaskID)
	}

	// 创建SSH客户端 - 使用context控制连接超时
	client, err := e.createSSHClientWithContext(ctx, req.Target, req.Port, req.Username, req.Password, req.PrivateKey)
	if err != nil {
		result.Success = false
		result.ErrorMessage = fmt.Sprintf("SSH连接失败: %v", err)
		result.EndTime = time.Now()
		result.ExecutionTime = result.EndTime.Sub(result.StartTime)
		e.logger.Errorf("SSH连接失败 - TaskID: %s, Error: %v", req.TaskID, err)
		return result, err
	}
	defer client.Close()

	// 上传脚本文件
	uploadStartTime := time.Now()
	remoteFilePath, err := e.uploadScript(client, req)
	if err != nil {
		result.Success = false
		result.ErrorMessage = fmt.Sprintf("脚本上传失败: %v", err)
		result.EndTime = time.Now()
		result.ExecutionTime = result.EndTime.Sub(result.StartTime)
		e.logger.Errorf("脚本上传失败 - TaskID: %s, Error: %v", req.TaskID, err)
		return result, err
	}
	result.RemoteFilePath = remoteFilePath
	result.UploadTime = time.Since(uploadStartTime)
	e.logger.Infof("脚本上传完成 - TaskID: %s, 用时: %v", req.TaskID, result.UploadTime)

	// 使用defer确保无论如何都会尝试清理脚本文件
	defer func() {
		if req.CleanupAfter && remoteFilePath != "" {
			if cleanupErr := e.cleanupScriptWithRetry(client, remoteFilePath); cleanupErr != nil {
				e.logger.Errorf("清理脚本文件最终失败 - TaskID: %s, Path: %s, Error: %v",
					req.TaskID, remoteFilePath, cleanupErr)
			} else {
				e.logger.Infof("脚本文件清理成功 - TaskID: %s, Path: %s",
					req.TaskID, remoteFilePath)
			}
		}
	}()

    // 创建SSH会话执行脚本
    session, err := client.NewSession()
    if err != nil {
		result.Success = false
		result.ErrorMessage = fmt.Sprintf("创建SSH会话失败: %v", err)
		result.EndTime = time.Now()
		result.ExecutionTime = result.EndTime.Sub(result.StartTime)
		return result, err
	}
    defer session.Close()

    // 按需为脚本申请 PTY
    if req.RequestPty {
        term := req.PtyTerm
        if term == "" { term = "xterm-256color" }
        rows := req.PtyRows; cols := req.PtyCols
        if rows <= 0 { rows = 40 }
        if cols <= 0 { cols = 120 }
        modes := ssh.TerminalModes{
            ssh.ECHO:          1,
            ssh.TTY_OP_ISPEED: 14400,
            ssh.TTY_OP_OSPEED: 14400,
        }
        if err := session.RequestPty(term, rows, cols, modes); err != nil {
            e.logger.Errorf("RequestPty 失败，退回无 TTY 模式: %v", err)
        }
    }

    // 设置环境变量（脚本执行同样兜底 export）
    var exportPrefix string
    if req.Environment != nil {
        var failed bool
        for key, value := range req.Environment {
            if err := session.Setenv(key, value); err != nil {
                failed = true
            }
        }
        if failed {
            var b strings.Builder
            for k, v := range req.Environment {
                v = strings.ReplaceAll(v, "'", "'\\''")
                fmt.Fprintf(&b, "export %s='%s'; ", k, v)
            }
            exportPrefix = b.String()
        }
    }

	// 构建脚本执行命令
    command := e.buildScriptCommand(req, remoteFilePath)
    if exportPrefix != "" {
        command = exportPrefix + command
    }
    if req.UseSudo {
        if req.SudoPreserveEnv {
            command = "__NB_SUDO_E__ " + command
        } else {
            command = "__NB_SUDO__ " + command
        }
        if req.SudoPassword != "" {
            os.Setenv("NB_SUDO_PWD", req.SudoPassword)
            defer os.Unsetenv("NB_SUDO_PWD")
        }
    }

    // 执行脚本（带 sudo/探测选项）
    stdout, stderr, exitCode, err := e.runCommand(ctx, session, command, req.UseSudo, req.SudoPreserveEnv, req.SudoPassword)

	endTime := time.Now()
	result.EndTime = endTime
	result.ExecutionTime = endTime.Sub(startTime)
	result.Stdout = stdout
	result.Stderr = stderr
	result.ExitCode = exitCode

	if err != nil {
		result.Success = false
		result.ErrorMessage = err.Error()
		e.logger.Errorf("脚本执行失败 - TaskID: %s, Error: %v", req.TaskID, err)
	} else {
		result.Success = true
		e.logger.Infof("脚本执行完成 - TaskID: %s, ExitCode: %d, Duration: %v",
			req.TaskID, exitCode, result.ExecutionTime)
	}

	// 添加元数据
	result.Metadata["target"] = fmt.Sprintf("%s:%d", req.Target, req.Port)
	result.Metadata["protocol"] = "ssh"
	result.Metadata["script_type"] = req.ScriptType
	result.Metadata["upload_time_ms"] = fmt.Sprintf("%.2f", result.UploadTime.Seconds()*1000)

	return result, nil
}

// createSSHClient 创建SSH客户端
func (e *SSHExecutor) createSSHClient(host string, port int32, username, password, privateKey string) (*ssh.Client, error) {
	return e.createSSHClientWithContext(context.Background(), host, port, username, password, privateKey)
}

// createSSHClientWithContext 创建SSH客户端（带context控制）
func (e *SSHExecutor) createSSHClientWithContext(ctx context.Context, host string, port int32, username, password, privateKey string) (*ssh.Client, error) {
	// 设置默认端口
	if port <= 0 {
		port = 22
	}

	// 设置连接超时，最大15秒
	connectTimeout := 15 * time.Second
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		remainingTime := time.Until(deadline)
		if remainingTime < connectTimeout {
			connectTimeout = remainingTime
		}
	}

	if connectTimeout <= 0 {
		return nil, fmt.Errorf("连接超时，没有足够的时间建立SSH连接")
	}

	e.logger.Infof("开始建立SSH连接 - 目标: %s:%d, 超时: %v", host, port, connectTimeout)

	config := &ssh.ClientConfig{
		User:            username,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 在生产环境中应该验证主机密钥
		Timeout:         connectTimeout,
	}

	// 根据认证方式配置
	if privateKey != "" {
		// 使用私钥认证
		signer, err := ssh.ParsePrivateKey([]byte(privateKey))
		if err != nil {
			return nil, fmt.Errorf("解析私钥失败: %v", err)
		}
		config.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
		e.logger.Infof("SSH使用私钥认证 - 用户: %s", username)
	} else if password != "" {
		// 使用密码认证
		config.Auth = []ssh.AuthMethod{ssh.Password(password)}
		e.logger.Infof("SSH使用密码认证 - 用户: %s", username)
	} else {
		return nil, fmt.Errorf("缺少认证信息")
	}

	// 连接SSH服务器
	addr := fmt.Sprintf("%s:%d", host, port)

	// 使用带超时的连接
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("连接SSH服务器失败: %v", err)
	}

	e.logger.Infof("SSH连接建立成功 - 目标: %s:%d", host, port)
	return client, nil
}

// buildCommand 构建完整的命令
func (e *SSHExecutor) buildCommand(req *types.CommandRequest) string {
    command := req.Command

	// 处理工作目录
	if req.WorkingDir != "" {
		command = fmt.Sprintf("cd %s && %s", req.WorkingDir, command)
	}

    // 不在这里处理 sudo；由执行阶段根据 NOPASSWD 探测决定是否注入密码
    
    return command
}

// buildScriptCommand 构建脚本执行命令
func (e *SSHExecutor) buildScriptCommand(req *types.ScriptRequest, scriptPath string) string {
	var command string

	// 确保脚本路径格式正确（使用正斜杠）
	scriptPath = strings.ReplaceAll(scriptPath, "\\", "/")

	// 根据脚本类型构建执行命令
	switch strings.ToLower(req.ScriptType) {
	case "bash":
		command = fmt.Sprintf("bash %s", scriptPath)
	case "sh", "shell":
		command = fmt.Sprintf("sh %s", scriptPath)
	case "python", "python3":
		command = fmt.Sprintf("python3 %s", scriptPath)
	case "python2":
		command = fmt.Sprintf("python2 %s", scriptPath)
	case "powershell", "ps1":
		command = fmt.Sprintf("powershell -File %s", scriptPath)
	default:
		// 默认使用bash
		command = fmt.Sprintf("bash %s", scriptPath)
	}

	// 处理工作目录
	if req.WorkingDir != "" {
		command = fmt.Sprintf("cd %s && %s", req.WorkingDir, command)
	}

    // 不在这里处理 sudo；由执行阶段根据 NOPASSWD 探测决定是否注入密码

    // 降低泄露风险：不打印完整命令，仅打印元信息
    // 日志脱敏：不打印脚本内容或完整命令
    e.logger.Infof("脚本执行 - TaskID: %s, ScriptType: %s, WorkingDir: %s, UseSudo: %t", req.TaskID, req.ScriptType, req.WorkingDir, req.UseSudo)
    return command
}

// runCommand 执行命令并返回结果
func (e *SSHExecutor) runCommand(ctx context.Context, session *ssh.Session, command string, needSudo bool, preserveEnv bool, sudoPassword string) (stdout, stderr string, exitCode int, err error) {
	startTime := time.Now()

	// 检查context超时设置
	var timeoutInfo string
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		timeout := time.Until(deadline)
		timeoutInfo = fmt.Sprintf("超时: %v", timeout)
    // 不打印完整命令，避免敏感泄露
    e.logger.Infof("SSH命令开始执行 - %s", timeoutInfo)
	} else {
		timeoutInfo = "无超时限制"
    e.logger.Infof("SSH命令开始执行 - %s", timeoutInfo)
	}

	// 创建输出缓冲区
	var stdoutBuf, stderrBuf strings.Builder
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

    // 构造一次性 shell 包装：先 sudo -n 尝试，再按需 -S 注入密码
    cmdErr := make(chan error, 1)
    go func() {
        cmdToRun := command
        if needSudo {
            eflag := ""
            if preserveEnv { eflag = "-E " }
            esc := strings.ReplaceAll(command, "\"", "\\\"")
            if sudoPassword != "" {
                // 为 sudo -S 提供 stdin
                session.Stdin = bytes.NewBufferString(sudoPassword + "\n")
                cmdToRun = fmt.Sprintf("bash -lc \"sudo -n %sbash -lc \"%s\" || sudo -S -p '' %sbash -lc \"%s\"\"", eflag, esc, eflag, esc)
            } else {
                cmdToRun = fmt.Sprintf("bash -lc \"sudo -n %sbash -lc \"%s\"\"", eflag, esc)
            }
        }
        cmdErr <- session.Run(cmdToRun)
    }()

	// 等待命令完成或超时
	select {
	case err = <-cmdErr:
		stdout = stdoutBuf.String()
		stderr = stderrBuf.String()
		duration := time.Since(startTime)

		// 获取退出码
		if err != nil {
			if exitError, ok := err.(*ssh.ExitError); ok {
				exitCode = exitError.ExitStatus()
            e.logger.Infof("SSH命令执行完成 - 退出码: %d, 用时: %v", exitCode, duration)
			} else {
				exitCode = -1
            e.logger.Errorf("SSH命令执行错误 - 用时: %v", duration)
			}
		} else {
			exitCode = 0
        e.logger.Infof("SSH命令执行成功 - 退出码: %d, 用时: %v", exitCode, duration)
		}

		return stdout, stderr, exitCode, nil

	case <-ctx.Done():
		duration := time.Since(startTime)
		// 超时或取消
		e.logger.Errorf("SSH命令执行超时或被取消 - 原因: %v, 已用时: %v", ctx.Err(), duration)

		// 尝试优雅终止
		e.logger.Info("尝试发送SIGTERM信号优雅终止命令")
		session.Signal(ssh.SIGTERM)
		time.Sleep(1 * time.Second)

		// 强制终止
		e.logger.Info("发送SIGKILL信号强制终止命令")
		session.Signal(ssh.SIGKILL)

		return stdoutBuf.String(), stderrBuf.String(), -1, fmt.Errorf("命令执行超时或被取消: %v", ctx.Err())
	}
}

// uploadScript 上传脚本文件到远程主机
func (e *SSHExecutor) uploadScript(client *ssh.Client, req *types.ScriptRequest) (string, error) {
	// 创建SFTP客户端
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return "", fmt.Errorf("创建SFTP客户端失败: %v", err)
	}
	defer sftpClient.Close()

	// 确定远程文件路径
	remoteFilePath := req.RemoteFilePath
	if remoteFilePath == "" {
		// 生成临时文件路径 - 直接使用TaskID，避免重复的"script"前缀
		fileName := fmt.Sprintf("%s_%d", req.TaskID, time.Now().Unix())
		switch strings.ToLower(req.ScriptType) {
		case "python", "python2", "python3":
			fileName += ".py"
		case "powershell", "ps1":
			fileName += ".ps1"
		case "bash":
			fileName += ".sh"
		default:
			fileName += ".sh"
		}
		// 使用path.Join而不是filepath.Join确保使用正斜杠
		remoteFilePath = path.Join("/tmp", fileName)
	}

	e.logger.Infof("脚本上传 - TaskID: %s, 远程路径: %s", req.TaskID, remoteFilePath)

	// 创建远程文件
	remoteFile, err := sftpClient.Create(remoteFilePath)
	if err != nil {
		return "", fmt.Errorf("创建远程文件失败: %v", err)
	}
	defer remoteFile.Close()

	// 写入脚本内容
	if _, err := io.WriteString(remoteFile, req.ScriptContent); err != nil {
		return "", fmt.Errorf("写入脚本内容失败: %v", err)
	}

	// 设置文件权限
	if req.RemoteFileMode != "" {
		if mode, parseErr := strconv.ParseUint(req.RemoteFileMode, 8, 32); parseErr == nil {
			if chmodErr := sftpClient.Chmod(remoteFilePath, os.FileMode(mode)); chmodErr != nil {
				e.logger.Infof("设置文件权限失败: %v", chmodErr)
			}
		}
	} else {
		// 默认设置为可执行
		if chmodErr := sftpClient.Chmod(remoteFilePath, 0755); chmodErr != nil {
			e.logger.Infof("设置默认文件权限失败: %v", chmodErr)
		}
	}

	e.logger.Infof("脚本上传成功 - TaskID: %s, 文件路径: %s", req.TaskID, remoteFilePath)
	return remoteFilePath, nil
}

// cleanupScript 清理脚本文件
func (e *SSHExecutor) cleanupScript(client *ssh.Client, scriptPath string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	command := fmt.Sprintf("rm -f %s", scriptPath)
	return session.Run(command)
}

// cleanupScriptWithRetry 带重试的脚本清理
func (e *SSHExecutor) cleanupScriptWithRetry(client *ssh.Client, scriptPath string) error {
	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := e.cleanupScript(client, scriptPath)
		if err == nil {
			return nil // 清理成功
		}

		e.logger.Infof("清理脚本文件第%d次尝试失败 - Path: %s, Error: %v",
			attempt, scriptPath, err)

		if attempt < maxRetries {
			// 等待一秒后重试
			time.Sleep(time.Second)
		}
	}

	return fmt.Errorf("清理脚本文件失败，已重试%d次", maxRetries)
}
