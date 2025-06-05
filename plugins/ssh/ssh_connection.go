package ssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"newbee-agent/plugins/common"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/crypto/ssh"
)

// SSHConnection SSH连接实现
type SSHConnection struct {
	// 基础信息
	id           string
	target       string
	protocol     string
	status       common.ConnectionStatus
	createdAt    time.Time
	lastActiveAt time.Time

	// SSH客户端
	sshClient  *ssh.Client
	sshSession *ssh.Session

	// 配置信息
	credentials *common.Credentials
	config      *SSHConfig

	// WebSocket桥接
	wsWriter io.Writer
	wsReader io.Reader
	wsBridge *webSocketBridge

	// 数据流管道
	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader

	// 会话管理
	session common.Session

	// 元数据
	metadata map[string]string

	// 同步控制
	mutex  sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	// 日志
	logger logx.Logger

	// 状态监控
	bytesRead    int64
	bytesWritten int64
}

// webSocketBridge WebSocket桥接器
type webSocketBridge struct {
	enabled     bool
	bufferSize  int
	encoding    string
	compression bool

	// 数据通道
	toWS   chan []byte
	fromWS chan []byte

	ctx    context.Context
	cancel context.CancelFunc
	logger logx.Logger
}

// NewSSHConnection 创建新的SSH连接
func NewSSHConnection(ctx context.Context, id string, target string, credentials *common.Credentials, config *SSHConfig) (common.Connection, error) {
	// 参数验证
	if ctx == nil {
		return nil, fmt.Errorf("context不能为nil")
	}
	if id == "" {
		return nil, fmt.Errorf("连接ID不能为空")
	}
	if target == "" {
		return nil, fmt.Errorf("目标地址不能为空")
	}
	if credentials == nil {
		return nil, fmt.Errorf("认证信息不能为nil")
	}
	if config == nil {
		return nil, fmt.Errorf("SSH配置不能为nil")
	}

	connCtx, cancel := context.WithCancel(ctx)
	logger := logx.WithContext(connCtx)

	logger.Infof("正在创建SSH连接: %s -> %s", id, target)

	conn := &SSHConnection{
		id:           id,
		target:       target,
		protocol:     "ssh",
		status:       common.StatusConnecting,
		createdAt:    time.Now(),
		lastActiveAt: time.Now(),
		credentials:  credentials,
		config:       config,
		metadata:     make(map[string]string),
		ctx:          connCtx,
		cancel:       cancel,
		logger:       logger,
	}

	// 使用defer确保在失败时清理资源
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("创建SSH连接 %s 时发生panic: %v", id, r)
			cancel()
		}
	}()

	// 初始化WebSocket桥接
	if config.WebSocketBridge != nil && config.WebSocketBridge.Enabled {
		conn.initWebSocketBridge()
		logger.Debugf("SSH连接 %s WebSocket桥接已初始化", id)
	}

	// 建立SSH连接
	if err := conn.connect(); err != nil {
		logger.Errorf("建立SSH连接 %s 失败: %v", id, err)
		cancel()
		return nil, fmt.Errorf("建立SSH连接失败: %w", err)
	}

	logger.Infof("SSH连接创建成功: %s -> %s", id, target)
	return conn, nil
}

// ID 返回连接ID
func (c *SSHConnection) ID() string {
	return c.id
}

// Target 返回目标地址
func (c *SSHConnection) Target() string {
	return c.target
}

// Protocol 返回协议类型
func (c *SSHConnection) Protocol() string {
	return c.protocol
}

// Status 返回连接状态
func (c *SSHConnection) Status() common.ConnectionStatus {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.status
}

// CreatedAt 返回创建时间
func (c *SSHConnection) CreatedAt() time.Time {
	return c.createdAt
}

// LastActiveAt 返回最后活跃时间
func (c *SSHConnection) LastActiveAt() time.Time {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.lastActiveAt
}

// Write 写入数据
func (c *SSHConnection) Write(data []byte) (int, error) {
	// 安全检查，防止空指针
	if c == nil {
		return 0, fmt.Errorf("SSH connection is nil")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// 检查连接状态
	if c.status != common.StatusConnected {
		c.logger.Errorf("尝试写入数据到非活跃连接: %s, 状态: %s", c.id, c.status)
		return 0, fmt.Errorf("connection is not active, status: %s", c.status)
	}

	// 检查stdin是否可用
	if c.stdin == nil {
		c.logger.Errorf("SSH连接 %s 的stdin不可用", c.id)
		return 0, fmt.Errorf("stdin not available for connection %s", c.id)
	}

	// 检查数据有效性
	if len(data) == 0 {
		c.logger.Debugf("SSH连接 %s 收到空数据", c.id)
		return 0, nil
	}

	c.updateLastActiveTime()

	// 写入数据并处理错误
	n, err := c.stdin.Write(data)
	if err != nil {
		c.logger.Errorf("SSH连接 %s 写入数据失败: %v", c.id, err)
		// 如果写入失败，可能连接已断开，更新状态
		c.status = common.StatusError
	} else {
		c.bytesWritten += int64(n)
		c.logger.Debugf("SSH连接 %s 成功写入 %d 字节", c.id, n)
	}

	return n, err
}

// Read 读取数据
func (c *SSHConnection) Read(data []byte) (int, error) {
	// 安全检查，防止空指针
	if c == nil {
		return 0, fmt.Errorf("SSH connection is nil")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// 检查连接状态
	if c.status != common.StatusConnected {
		c.logger.Errorf("尝试从非活跃连接读取数据: %s, 状态: %s", c.id, c.status)
		return 0, fmt.Errorf("connection is not active, status: %s", c.status)
	}

	// 检查stdout是否可用
	if c.stdout == nil {
		c.logger.Errorf("SSH连接 %s 的stdout不可用", c.id)
		return 0, fmt.Errorf("stdout not available for connection %s", c.id)
	}

	// 检查缓冲区有效性
	if len(data) == 0 {
		c.logger.Debugf("SSH连接 %s 提供的读取缓冲区为空", c.id)
		return 0, nil
	}

	c.updateLastActiveTime()

	// 读取数据并处理错误
	n, err := c.stdout.Read(data)
	if err != nil {
		if err != io.EOF {
			c.logger.Errorf("SSH连接 %s 读取数据失败: %v", c.id, err)
			// 如果读取失败且不是EOF，可能连接已断开
			c.status = common.StatusError
		} else {
			c.logger.Debugf("SSH连接 %s 到达EOF", c.id)
		}
	} else {
		c.bytesRead += int64(n)
		c.logger.Debugf("SSH连接 %s 成功读取 %d 字节", c.id, n)
	}

	return n, err
}

// SetWebSocketWriter 设置WebSocket写入器
func (c *SSHConnection) SetWebSocketWriter(writer io.Writer) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.wsWriter = writer

	// 直接启动SSH输出到WebSocket的桥接
	if c.stdout != nil && c.stderr != nil {
		go c.bridgeSSHToWebSocket(writer)
	}

	return nil
}

// SetWebSocketReader 设置WebSocket读取器
func (c *SSHConnection) SetWebSocketReader(reader io.Reader) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.wsReader = reader

	// 直接启动WebSocket输入到SSH的桥接
	if c.stdin != nil {
		go c.bridgeWebSocketToSSH(reader)
	}

	return nil
}

// bridgeSSHToWebSocket 将SSH输出桥接到WebSocket
func (c *SSHConnection) bridgeSSHToWebSocket(writer io.Writer) {
	defer c.logger.Info("SSH to WebSocket bridge stopped")

	buffer := make([]byte, 4096)

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("SSH to WebSocket bridge context cancelled")
			return
		default:
			// 设置读取超时，避免阻塞
			if c.sshSession != nil {
				// 使用带超时的读取
				n, err := c.stdout.Read(buffer)
				if err != nil {
					if err != io.EOF {
						c.logger.Errorf("Error reading from SSH stdout: %v", err)
					}
					return
				}

				if n > 0 {
					c.logger.Infof("Read from SSH: %s (length: %d)", string(buffer[:n]), n)

					if writer != nil {
						if _, err := writer.Write(buffer[:n]); err != nil {
							c.logger.Errorf("Error writing to WebSocket: %v", err)
							return
						}
					}

					c.updateLastActiveTime()
					c.bytesRead += int64(n)
				}
			} else {
				// SSH会话不存在，退出循环
				c.logger.Errorf("SSH session is nil, stopping bridge")
				return
			}
		}
	}
}

func (c *SSHConnection) bridgeWebSocketToSSH(reader io.Reader) {
	defer c.logger.Info("WebSocket to SSH bridge stopped")

	buffer := make([]byte, 4096)

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("WebSocket to SSH bridge context cancelled")
			return
		default:
			// 检查SSH连接状态
			if c.stdin == nil {
				c.logger.Errorf("SSH stdin is nil, stopping bridge")
				return
			}

			n, err := reader.Read(buffer)
			if err != nil {
				if err != io.EOF {
					c.logger.Errorf("Error reading from WebSocket: %v", err)
				}
				return
			}

			if n > 0 {
				c.logger.Infof("Received from WebSocket: %s (length: %d)", string(buffer[:n]), n)

				if _, err := c.stdin.Write(buffer[:n]); err != nil {
					c.logger.Errorf("Error writing to SSH stdin: %v", err)
					return
				}

				c.logger.Debugf("Successfully sent %d bytes to SSH", n)
				c.updateLastActiveTime()
				c.bytesWritten += int64(n)
			}
		}
	}
}

// Close 关闭连接
func (c *SSHConnection) Close() error {
	// 安全检查，防止空指针
	if c == nil {
		return fmt.Errorf("SSH connection is nil")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.logger.Infof("正在关闭SSH连接: %s", c.id)

	// 防止重复关闭
	if c.status == common.StatusDisconnected {
		c.logger.Debugf("SSH连接 %s 已经关闭", c.id)
		return nil
	}

	// 更新状态
	c.status = common.StatusDisconnected

	// 使用defer确保资源清理，即使出现panic也能执行
	defer func() {
		if r := recover(); r != nil {
			c.logger.Errorf("SSH连接 %s 关闭过程中发生panic: %v", c.id, r)
		}
	}()

	// 取消上下文
	if c.cancel != nil {
		c.cancel()
		c.logger.Debugf("SSH连接 %s 上下文已取消", c.id)
	}

	// 关闭WebSocket桥接
	if c.wsBridge != nil && c.wsBridge.cancel != nil {
		c.wsBridge.cancel()
		c.logger.Debugf("SSH连接 %s WebSocket桥接已关闭", c.id)
	}

	// 关闭数据流
	if c.stdin != nil {
		if err := c.stdin.Close(); err != nil {
			c.logger.Errorf("关闭SSH连接 %s stdin失败: %v", c.id, err)
		}
	}

	// 关闭SSH会话
	if c.sshSession != nil {
		if err := c.sshSession.Close(); err != nil {
			c.logger.Errorf("关闭SSH会话 %s 失败: %v", c.id, err)
		} else {
			c.logger.Debugf("SSH会话 %s 已关闭", c.id)
		}
		c.sshSession = nil
	}

	// 关闭SSH客户端
	if c.sshClient != nil {
		if err := c.sshClient.Close(); err != nil {
			c.logger.Errorf("关闭SSH客户端 %s 失败: %v", c.id, err)
		} else {
			c.logger.Debugf("SSH客户端 %s 已关闭", c.id)
		}
		c.sshClient = nil
	}

	c.logger.Infof("SSH连接已关闭: %s", c.id)
	return nil
}

// IsConnected 检查是否已连接
func (c *SSHConnection) IsConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.status == common.StatusConnected && c.sshClient != nil
}

// GetSession 获取会话
func (c *SSHConnection) GetSession() common.Session {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.session
}

// GetMetadata 获取元数据
func (c *SSHConnection) GetMetadata() map[string]string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// 返回副本
	metadata := make(map[string]string)
	for k, v := range c.metadata {
		metadata[k] = v
	}

	return metadata
}

// SetMetadata 设置元数据
func (c *SSHConnection) SetMetadata(key, value string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.metadata[key] = value
}

// connect 建立SSH连接
func (c *SSHConnection) connect() error {
	c.logger.Infof("Connecting to SSH server: %s", c.target)

	// 解析目标地址
	host, port, err := c.parseTarget(c.target)
	if err != nil {
		c.status = common.StatusError
		return fmt.Errorf("invalid target address: %w", err)
	}

	// 构建SSH客户端配置
	sshConfig, err := c.buildSSHConfig()
	if err != nil {
		c.status = common.StatusError
		return fmt.Errorf("failed to build SSH config: %w", err)
	}

	// 建立TCP连接
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, c.config.ConnectionTimeout)
	if err != nil {
		c.status = common.StatusTimeout
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	// 建立SSH连接
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
	if err != nil {
		conn.Close()
		c.status = common.StatusError
		return fmt.Errorf("SSH handshake failed: %w", err)
	}

	// 创建SSH客户端
	c.sshClient = ssh.NewClient(sshConn, chans, reqs)

	// 创建SSH会话
	session, err := c.sshClient.NewSession()
	if err != nil {
		c.sshClient.Close()
		c.status = common.StatusError
		return fmt.Errorf("failed to create SSH session: %w", err)
	}

	c.sshSession = session

	// 设置会话模式
	if err := c.setupSession(); err != nil {
		c.Close()
		c.status = common.StatusError
		return fmt.Errorf("failed to setup SSH session: %w", err)
	}

	// 更新状态
	c.status = common.StatusConnected
	c.updateLastActiveTime()

	// 启动数据处理
	go c.startDataProcessing()

	c.logger.Infof("SSH connection established: %s -> %s", c.id, c.target)
	return nil
}

// parseTarget 解析目标地址
func (c *SSHConnection) parseTarget(target string) (string, int, error) {
	if strings.Contains(target, ":") {
		host, portStr, err := net.SplitHostPort(target)
		if err != nil {
			return "", 0, err
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			return "", 0, fmt.Errorf("invalid port: %s", portStr)
		}

		return host, port, nil
	}

	// 默认SSH端口
	return target, 22, nil
}

// buildSSHConfig 构建SSH客户端配置
func (c *SSHConnection) buildSSHConfig() (*ssh.ClientConfig, error) {
	config := &ssh.ClientConfig{
		User:            c.credentials.Username,
		Timeout:         c.config.ConnectionTimeout,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 生产环境应该验证主机密钥
	}

	// 配置认证方式
	switch c.credentials.AuthType {
	case "password":
		config.Auth = []ssh.AuthMethod{
			ssh.Password(c.credentials.Password),
		}
	case "publickey":
		authMethod, err := c.buildPublicKeyAuth()
		if err != nil {
			return nil, fmt.Errorf("failed to build public key auth: %w", err)
		}
		config.Auth = []ssh.AuthMethod{authMethod}
	case "keyboard-interactive":
		// 键盘交互式认证
		config.Auth = []ssh.AuthMethod{
			ssh.KeyboardInteractive(c.keyboardInteractiveAuth),
		}
	default:
		return nil, fmt.Errorf("unsupported auth type: %s", c.credentials.AuthType)
	}

	return config, nil
}

// buildPublicKeyAuth 构建公钥认证
func (c *SSHConnection) buildPublicKeyAuth() (ssh.AuthMethod, error) {
	var keyData []byte
	var err error

	if c.credentials.PrivateKey != "" {
		keyData = []byte(c.credentials.PrivateKey)
	} else if c.credentials.KeyFile != "" {
		// 这里应该读取密钥文件，但为了简化，暂时不实现
		return nil, fmt.Errorf("key file reading not implemented")
	} else {
		return nil, fmt.Errorf("no private key provided")
	}

	var signer ssh.Signer
	if c.credentials.Password != "" {
		// 带密码的私钥
		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(c.credentials.Password))
	} else {
		// 无密码的私钥
		signer, err = ssh.ParsePrivateKey(keyData)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return ssh.PublicKeys(signer), nil
}

// keyboardInteractiveAuth 键盘交互式认证
func (c *SSHConnection) keyboardInteractiveAuth(user, instruction string, questions []string, echos []bool) ([]string, error) {
	// 简化实现，只处理密码提示
	answers := make([]string, len(questions))
	for i := range questions {
		if strings.Contains(strings.ToLower(questions[i]), "password") {
			answers[i] = c.credentials.Password
		}
	}
	return answers, nil
}

// setupSession 设置SSH会话
func (c *SSHConnection) setupSession() error {
	// 请求PTY
	if err := c.sshSession.RequestPty("xterm-256color", 80, 24, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		return fmt.Errorf("failed to request PTY: %w", err)
	}

	// 获取输入输出流
	stdin, err := c.sshSession.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin: %w", err)
	}
	c.stdin = stdin

	stdout, err := c.sshSession.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout: %w", err)
	}
	c.stdout = stdout

	stderr, err := c.sshSession.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr: %w", err)
	}
	c.stderr = stderr

	// 启动shell
	if err := c.sshSession.Shell(); err != nil {
		return fmt.Errorf("failed to start shell: %w", err)
	}

	return nil
}

// startDataProcessing 启动数据处理
func (c *SSHConnection) startDataProcessing() {
	// 只启动会话监控，WebSocket桥接在SetWebSocketReader/Writer中单独处理
	go c.monitorSession()
}

// monitorSession 监控会话状态
func (c *SSHConnection) monitorSession() {
	defer func() {
		c.mutex.Lock()
		c.status = common.StatusDisconnected
		c.mutex.Unlock()
		c.logger.Infof("SSH session monitoring stopped: %s", c.id)
	}()

	// 等待SSH会话结束
	if err := c.sshSession.Wait(); err != nil {
		c.logger.Errorf("SSH session error: %v", err)
		c.mutex.Lock()
		c.status = common.StatusError
		c.mutex.Unlock()
	}
}

// updateLastActiveTime 更新最后活跃时间
func (c *SSHConnection) updateLastActiveTime() {
	c.lastActiveAt = time.Now()
}

// initWebSocketBridge 初始化WebSocket桥接（已弃用）
func (c *SSHConnection) initWebSocketBridge() {
	// WebSocket桥接现在直接在SetWebSocketReader/Writer中处理
	// 保留此方法以保持接口兼容性，但不做任何操作
}
