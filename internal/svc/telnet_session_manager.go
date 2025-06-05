package svc

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// TelnetTunnelRequest Telnet隧道请求参数（避免导入循环）
type TelnetTunnelRequest struct {
	Target    string `json:"target"`     // Telnet服务器地址
	Port      int    `json:"port"`       // Telnet端口
	Username  string `json:"username"`   // 用户名（可选，有些设备只需密码）
	Password  string `json:"password"`   // 密码
	Cols      int    `json:"cols"`       // 终端列数
	Rows      int    `json:"rows"`       // 终端行数
	SessionID string `json:"session_id"` // 会话ID
}

// SafeWebSocketWriter 接口（避免导入循环）
type SafeWebSocketWriter interface {
	Write(p []byte) (n int, err error)
	WriteJSON(v interface{}) error
	Close()
}

// TelnetSession Telnet会话
type TelnetSession struct {
	ID              string
	Target          string
	Port            int
	Username        string
	Password        string
	Conn            net.Conn
	Logger          logx.Logger
	Ctx             context.Context
	Cancel          context.CancelFunc
	CreatedAt       time.Time
	LastActiveAt    time.Time
	IsAuthenticated bool
	mutex           sync.RWMutex
}

// TelnetSessionManager Telnet会话管理器
type TelnetSessionManager struct {
	sessions map[string]*TelnetSession
	mutex    sync.RWMutex
	logger   logx.Logger
	counter  int64
}

// NewTelnetSessionManager 创建Telnet会话管理器
func NewTelnetSessionManager(logger logx.Logger) *TelnetSessionManager {
	return &TelnetSessionManager{
		sessions: make(map[string]*TelnetSession),
		logger:   logger,
	}
}

// CreateSession 创建Telnet会话
func (m *TelnetSessionManager) CreateSession(ctx context.Context, req *TelnetTunnelRequest) (*TelnetSession, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 生成会话ID
	m.counter++
	sessionID := fmt.Sprintf("telnet_session_%d_%d", time.Now().Unix(), m.counter)

	// 创建会话上下文
	sessionCtx, cancel := context.WithCancel(ctx)

	// 创建Telnet连接
	address := fmt.Sprintf("%s:%d", req.Target, req.Port)
	m.logger.Infof("创建Telnet连接到: %s", address)

	dialer := &net.Dialer{
		Timeout: 15 * time.Second,
	}

	conn, err := dialer.DialContext(sessionCtx, "tcp", address)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("连接到 %s 失败: %w", address, err)
	}

	// 创建会话对象
	session := &TelnetSession{
		ID:           sessionID,
		Target:       req.Target,
		Port:         req.Port,
		Username:     req.Username,
		Password:     req.Password,
		Conn:         conn,
		Logger:       m.logger,
		Ctx:          sessionCtx,
		Cancel:       cancel,
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
	}

	// 执行认证
	if err := session.authenticate(); err != nil {
		conn.Close()
		cancel()
		return nil, fmt.Errorf("认证失败: %w", err)
	}

	// 保存会话
	m.sessions[sessionID] = session
	m.logger.Infof("Telnet会话创建成功: %s -> %s", sessionID, address)

	return session, nil
}

// GetSession 获取会话
func (m *TelnetSessionManager) GetSession(sessionID string) (*TelnetSession, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	session, exists := m.sessions[sessionID]
	return session, exists
}

// CloseSession 关闭会话
func (m *TelnetSessionManager) CloseSession(sessionID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if session, exists := m.sessions[sessionID]; exists {
		session.Close()
		delete(m.sessions, sessionID)
		m.logger.Infof("Telnet会话已关闭: %s", sessionID)
	}
}

// GetActiveSessionCount 获取活跃会话数
func (m *TelnetSessionManager) GetActiveSessionCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.sessions)
}

// Close 关闭所有会话
func (m *TelnetSessionManager) Close() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for id, session := range m.sessions {
		session.Close()
		delete(m.sessions, id)
	}
	m.logger.Info("所有Telnet会话已关闭")
}

// authenticate 执行Telnet认证
func (s *TelnetSession) authenticate() error {
	s.Logger.Info("开始Telnet认证流程")

	// 设置读取超时
	s.Conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	defer s.Conn.SetReadDeadline(time.Time{})

	// 读取初始输出并进行协议协商
	buffer := make([]byte, 4096)
	var allOutput strings.Builder

	// 多次读取以获取完整的初始输出
	for attempts := 0; attempts < 5; attempts++ {
		s.Conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := s.Conn.Read(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				s.Logger.Infof("读取超时 (尝试 %d/5)", attempts+1)
				if attempts == 0 {
					continue // 第一次超时继续尝试
				}
				break // 后续超时则跳出
			}
			return fmt.Errorf("读取初始输出失败: %w", err)
		}

		if n > 0 {
			rawData := string(buffer[:n])
			s.Logger.Infof("收到原始数据 #%d (%d bytes): %q", attempts+1, n, rawData)

			// 处理Telnet协议并获取清理后的数据
			cleanData := s.processTelnetProtocolAdvanced(rawData)
			allOutput.WriteString(cleanData)

			s.Logger.Infof("清理后数据 #%d: %q", attempts+1, cleanData)

			// 检查是否包含提示符
			currentOutput := allOutput.String()
			if s.containsPrompt(currentOutput) {
				s.Logger.Infof("检测到提示符，停止读取")
				break
			}
		}

		// 短暂等待更多数据
		time.Sleep(500 * time.Millisecond)
	}

	initialOutput := allOutput.String()
	s.Logger.Infof("完整初始输出: %q", initialOutput)

	// 根据用户名是否为空采用不同的认证策略
	if s.Username != "" {
		// 用户名+密码认证
		if err := s.performUsernamePasswordAuthAdvanced(initialOutput); err != nil {
			return err
		}
	} else if s.Password != "" {
		// 仅密码认证
		if err := s.performPasswordOnlyAuthAdvanced(initialOutput); err != nil {
			return err
		}
	} else {
		// 检查是否已经有命令提示符
		if s.hasCommandPrompt(initialOutput) {
			s.Logger.Info("检测到命令提示符，无需认证")
		} else {
			s.Logger.Infof("无认证信息且未检测到命令提示符")
		}
	}

	s.IsAuthenticated = true
	s.Logger.Info("Telnet认证成功")
	return nil
}

// processTelnetProtocolAdvanced 高级Telnet协议处理
func (s *TelnetSession) processTelnetProtocolAdvanced(data string) string {
	var result []byte
	i := 0

	for i < len(data) {
		if i < len(data) && data[i] == '\xff' { // IAC
			if i+1 < len(data) {
				cmd := data[i+1]
				switch cmd {
				case '\xfe': // DON'T
					if i+2 < len(data) {
						option := data[i+2]
						s.Logger.Debugf("收到DON'T，选项: %d", option)
						// 回应WON'T
						response := []byte{'\xff', '\xfc', option}
						s.Conn.Write(response)
						i += 3
					} else {
						i += 2
					}
				case '\xfd': // DO
					if i+2 < len(data) {
						option := data[i+2]
						s.Logger.Debugf("收到DO，选项: %d", option)
						// 回应WON'T (除非支持该选项)
						response := []byte{'\xff', '\xfc', option}
						s.Conn.Write(response)
						i += 3
					} else {
						i += 2
					}
				case '\xfc': // WON'T
					if i+2 < len(data) {
						option := data[i+2]
						s.Logger.Debugf("收到WON'T，选项: %d", option)
						i += 3
					} else {
						i += 2
					}
				case '\xfb': // WILL
					if i+2 < len(data) {
						option := data[i+2]
						s.Logger.Debugf("收到WILL，选项: %d", option)
						// 回应DON'T (除非需要该选项)
						response := []byte{'\xff', '\xfe', option}
						s.Conn.Write(response)
						i += 3
					} else {
						i += 2
					}
				default:
					s.Logger.Debugf("跳过未知IAC命令: %d", cmd)
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

// containsPrompt 检查是否包含任何类型的提示符
func (s *TelnetSession) containsPrompt(output string) bool {
	lower := strings.ToLower(output)

	// 检查登录提示符
	if strings.Contains(lower, "login:") || strings.Contains(lower, "username:") {
		return true
	}

	// 检查密码提示符
	if strings.Contains(lower, "password:") {
		return true
	}

	// 检查命令提示符
	if s.hasCommandPrompt(output) {
		return true
	}

	return false
}

// hasCommandPrompt 检查是否包含命令提示符
func (s *TelnetSession) hasCommandPrompt(output string) bool {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, "#") || strings.HasSuffix(trimmed, "$") ||
			strings.HasSuffix(trimmed, ">") || strings.HasSuffix(trimmed, "%") {
			if len(trimmed) < 100 { // 合理的提示符长度
				return true
			}
		}
	}
	return false
}

// performUsernamePasswordAuthAdvanced 高级用户名+密码认证
func (s *TelnetSession) performUsernamePasswordAuthAdvanced(initialOutput string) error {
	s.Logger.Info("开始用户名+密码认证")

	// 检查初始状态
	if s.hasCommandPrompt(initialOutput) {
		s.Logger.Info("已在命令提示符，无需认证")
		return nil
	}

	// 如果还没有登录提示符，等待一下
	if !strings.Contains(strings.ToLower(initialOutput), "login") {
		s.Logger.Info("等待登录提示符...")
		output, err := s.readUntilPrompt([]string{"login:", "username:"}, 10*time.Second)
		if err != nil {
			s.Logger.Errorf("等待登录提示符失败: %v", err)
			// 尝试直接发送用户名
		}
		initialOutput += output
	}

	// 发送用户名
	s.Logger.Infof("发送用户名: %s", s.Username)
	if _, err := s.Conn.Write([]byte(s.Username + "\r\n")); err != nil {
		return fmt.Errorf("发送用户名失败: %w", err)
	}

	// 等待密码提示符
	s.Logger.Info("等待密码提示符...")
	passwordOutput, err := s.readUntilPrompt([]string{"password:"}, 10*time.Second)
	if err != nil {
		s.Logger.Errorf("等待密码提示符失败: %v", err)
		s.Logger.Info("尝试直接发送密码...")
	} else {
		s.Logger.Infof("收到密码提示: %q", passwordOutput)
	}

	// 发送密码
	s.Logger.Info("发送密码...")
	if _, err := s.Conn.Write([]byte(s.Password + "\r\n")); err != nil {
		return fmt.Errorf("发送密码失败: %w", err)
	}

	// 等待命令提示符
	s.Logger.Info("等待命令提示符...")
	commandOutput, err := s.readUntilPrompt([]string{"#", "$", ">", "%"}, 10*time.Second)
	if err != nil {
		s.Logger.Errorf("等待命令提示符失败: %v", err)
		s.Logger.Infof("收到的输出: %q", commandOutput)
		return fmt.Errorf("认证可能失败: %w", err)
	}

	s.Logger.Infof("认证成功，收到命令提示符: %q", commandOutput)
	return nil
}

// performPasswordOnlyAuthAdvanced 高级仅密码认证
func (s *TelnetSession) performPasswordOnlyAuthAdvanced(initialOutput string) error {
	s.Logger.Info("开始仅密码认证")

	// 检查初始状态
	if s.hasCommandPrompt(initialOutput) {
		s.Logger.Info("已在命令提示符，无需认证")
		return nil
	}

	// 等待密码提示符
	if !strings.Contains(strings.ToLower(initialOutput), "password") {
		s.Logger.Info("等待密码提示符...")
		output, err := s.readUntilPrompt([]string{"password:"}, 10*time.Second)
		if err != nil {
			s.Logger.Errorf("等待密码提示符失败: %v", err)
		}
		initialOutput += output
	}

	// 发送密码
	s.Logger.Info("发送密码...")
	if _, err := s.Conn.Write([]byte(s.Password + "\r\n")); err != nil {
		return fmt.Errorf("发送密码失败: %w", err)
	}

	// 等待命令提示符
	s.Logger.Info("等待命令提示符...")
	commandOutput, err := s.readUntilPrompt([]string{"#", "$", ">", "%"}, 10*time.Second)
	if err != nil {
		s.Logger.Errorf("等待命令提示符失败: %v", err)
		return fmt.Errorf("认证可能失败: %w", err)
	}

	s.Logger.Infof("认证成功，收到命令提示符: %q", commandOutput)
	return nil
}

// readUntilPrompt 读取直到遇到指定的提示符
func (s *TelnetSession) readUntilPrompt(prompts []string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var output strings.Builder
	buffer := make([]byte, 1024)

	for time.Now().Before(deadline) {
		s.Conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, err := s.Conn.Read(buffer)

		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // 继续等待
			}
			return output.String(), err
		}

		if n > 0 {
			rawData := string(buffer[:n])
			cleanData := s.processTelnetProtocolAdvanced(rawData)
			output.WriteString(cleanData)

			// 检查是否包含任何提示符
			currentOutput := strings.ToLower(output.String())
			for _, prompt := range prompts {
				if strings.Contains(currentOutput, strings.ToLower(prompt)) {
					return output.String(), nil
				}
			}

			// 检查命令提示符模式
			if s.hasCommandPrompt(output.String()) {
				return output.String(), nil
			}
		}
	}

	return output.String(), fmt.Errorf("超时等待提示符: %v", prompts)
}

// StartForwarding 启动数据转发到WebSocket
func (s *TelnetSession) StartForwarding(wsWriter SafeWebSocketWriter) {
	s.Logger.Info("启动Telnet到WebSocket数据转发")

	go func() {
		defer func() {
			s.Logger.Info("Telnet到WebSocket转发结束")
		}()

		buffer := make([]byte, 4096)
		for {
			select {
			case <-s.Ctx.Done():
				return
			default:
			}

			// 设置读取超时
			s.Conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, err := s.Conn.Read(buffer)
			if err != nil {
				if err != io.EOF && !isTimeoutError(err) {
					s.Logger.Errorf("从Telnet读取数据失败: %v", err)
				}
				continue
			}

			// 更新活跃时间
			s.updateLastActive()

			// 处理Telnet协议字符
			cleanData := s.processTelnetProtocolAdvanced(string(buffer[:n]))

			// 转发到WebSocket
			if len(cleanData) > 0 {
				if _, err := wsWriter.Write([]byte(cleanData)); err != nil {
					s.Logger.Errorf("写入WebSocket失败: %v", err)
					return
				}
			}
		}
	}()
}

// SendToTelnet 发送数据到Telnet连接
func (s *TelnetSession) SendToTelnet(data []byte) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.updateLastActive()

	// 发送数据到Telnet连接
	_, err := s.Conn.Write(data)
	if err != nil {
		return fmt.Errorf("发送数据到Telnet失败: %w", err)
	}

	return nil
}

// updateLastActive 更新最后活跃时间
func (s *TelnetSession) updateLastActive() {
	s.LastActiveAt = time.Now()
}

// Close 关闭会话
func (s *TelnetSession) Close() {
	s.Cancel()
	if s.Conn != nil {
		s.Conn.Close()
	}
}

// isTimeoutError 检查是否为超时错误
func isTimeoutError(err error) bool {
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}
