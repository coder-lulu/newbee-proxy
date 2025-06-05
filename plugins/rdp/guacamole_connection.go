package rdp

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"newbee-agent/plugins/common"

	"github.com/zeromicro/go-zero/core/logx"
)

// GuacamoleConnection Guacamole RDP连接实现 - 基于mayfly-go的简化版本
type GuacamoleConnection struct {
	// 基础信息
	id           string
	target       string
	status       common.ConnectionStatus
	createdAt    time.Time
	lastActiveAt time.Time

	// Guacamole相关
	guacdConn   net.Conn
	tunnel      *GuacamoleStream
	config      *GuacamoleConfiguration
	credentials *common.Credentials

	// 会话管理
	session  common.Session
	metadata map[string]string

	// 同步控制
	mutex  sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	// 日志
	logger logx.Logger

	// 统计信息
	bytesRead    int64
	bytesWritten int64
}

// NewGuacamoleConnection 创建新的Guacamole连接 - 基于mayfly-go的简化实现
func NewGuacamoleConnection(id, target string, credentials *common.Credentials, guacdManager *GuacdManager) (*GuacamoleConnection, error) {
	ctx, cancel := context.WithCancel(context.Background())
	logger := logx.WithContext(ctx)

	conn := &GuacamoleConnection{
		id:          id,
		target:      target,
		status:      common.StatusConnecting,
		createdAt:   time.Now(),
		credentials: credentials,
		metadata:    make(map[string]string),
		ctx:         ctx,
		cancel:      cancel,
		logger:      logger,
	}

	// 建立连接
	if err := conn.connect(guacdManager); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to establish guacamole RDP connection: %w", err)
	}

	conn.status = common.StatusConnected
	conn.lastActiveAt = time.Now()

	logger.Infof("Guacamole RDP连接建立成功: %s", id)
	return conn, nil
}

// connect 建立Guacamole连接 - 基于mayfly-go的连接流程
func (gc *GuacamoleConnection) connect(guacdManager *GuacdManager) error {
	gc.logger.Infof("开始建立Guacamole RDP连接: %s", gc.id)

	// 获取到guacd的连接
	guacdConn, err := guacdManager.GetConnection()
	if err != nil {
		return fmt.Errorf("无法连接到guacd: %w", err)
	}

	gc.guacdConn = guacdConn

	// 创建Guacamole流
	gc.tunnel = NewGuacamoleStream(guacdConn, SocketTimeout)

	// 准备配置
	gc.config = gc.buildConfiguration()

	// 执行握手
	if err := gc.tunnel.Handshake(gc.config); err != nil {
		guacdConn.Close()
		return fmt.Errorf("guacamole handshake failed: %w", err)
	}

	gc.logger.Infof("Guacamole RDP连接握手成功: %s, 连接ID: %s", gc.id, gc.tunnel.ConnectionID)
	return nil
}

// buildConfiguration 构建Guacamole配置 - 基于mayfly-go的配置方式
func (gc *GuacamoleConnection) buildConfiguration() *GuacamoleConfiguration {
	config := NewGuacamoleConfiguration()
	config.Protocol = "rdp"

	// 从target解析主机和端口
	host, port, _ := parseTarget(gc.target)

	// 基本连接参数
	config.Parameters["hostname"] = host
	config.Parameters["port"] = fmt.Sprintf("%d", port)
	config.Parameters["username"] = gc.credentials.Username
	config.Parameters["password"] = gc.credentials.Password

	// 设置分辨率
	config.OptimalScreenWidth = 1920
	config.OptimalScreenHeight = 1080
	config.OptimalResolution = 96

	config.Parameters["width"] = fmt.Sprintf("%d", config.OptimalScreenWidth)
	config.Parameters["height"] = fmt.Sprintf("%d", config.OptimalScreenHeight)

	// RDP性能优化参数 - 参考mayfly-go的优化配置
	config.Parameters["scheme"] = "rdp"
	config.Parameters["color-depth"] = "32"
	config.Parameters["resize-method"] = "display-update"
	config.Parameters["force-lossless"] = "true"
	config.Parameters["ignore-cert"] = "true"

	// 客户端优化
	config.Parameters["client-name"] = "newbee-agent"
	config.Parameters["enable-wallpaper"] = "true"
	config.Parameters["enable-font-smoothing"] = "true"
	config.Parameters["enable-desktop-composition"] = "false"
	config.Parameters["enable-menu-animations"] = "false"
	config.Parameters["disable-bitmap-caching"] = "true"
	config.Parameters["disable-offscreen-caching"] = "true"

	// 文件共享配置 - 使用临时目录或禁用
	config.Parameters["enable-drive"] = "false" // 暂时禁用文件共享避免路径问题
	// config.Parameters["drive-name"] = "Filesystem"
	// config.Parameters["drive-path"] = "/tmp/rdp-shared"
	// config.Parameters["create-drive-path"] = "true"

	return config
}

// Read 读取数据
func (gc *GuacamoleConnection) Read(p []byte) (n int, err error) {
	if gc.tunnel == nil {
		return 0, fmt.Errorf("connection not established")
	}

	data, err := gc.tunnel.ReadSome()
	if err != nil {
		return 0, err
	}

	n = copy(p, data)
	gc.bytesRead += int64(n)
	gc.updateLastActive()

	return n, nil
}

// Write 写入数据
func (gc *GuacamoleConnection) Write(p []byte) (n int, err error) {
	if gc.tunnel == nil {
		return 0, fmt.Errorf("connection not established")
	}

	n, err = gc.tunnel.Write(p)
	if err != nil {
		return n, err
	}

	gc.bytesWritten += int64(n)
	gc.updateLastActive()

	return n, nil
}

// Close 关闭连接
func (gc *GuacamoleConnection) Close() error {
	gc.mutex.Lock()
	defer gc.mutex.Unlock()

	if gc.status == common.StatusDisconnected {
		return nil
	}

	gc.status = common.StatusDisconnected
	gc.cancel()

	// 关闭Guacamole流
	if gc.tunnel != nil {
		gc.tunnel.Close()
	}

	// 关闭底层连接
	if gc.guacdConn != nil {
		gc.guacdConn.Close()
	}

	gc.logger.Infof("Guacamole RDP连接已关闭: %s", gc.id)
	return nil
}

// 实现common.Connection接口的其他方法
func (gc *GuacamoleConnection) ID() string {
	return gc.id
}

func (gc *GuacamoleConnection) Target() string {
	return gc.target
}

func (gc *GuacamoleConnection) Protocol() string {
	return "rdp"
}

func (gc *GuacamoleConnection) Status() common.ConnectionStatus {
	gc.mutex.RLock()
	defer gc.mutex.RUnlock()
	return gc.status
}

func (gc *GuacamoleConnection) CreatedAt() time.Time {
	return gc.createdAt
}

func (gc *GuacamoleConnection) LastActiveAt() time.Time {
	gc.mutex.RLock()
	defer gc.mutex.RUnlock()
	return gc.lastActiveAt
}

func (gc *GuacamoleConnection) GetSession() common.Session {
	return gc.session
}

func (gc *GuacamoleConnection) IsConnected() bool {
	return gc.Status() == common.StatusConnected
}

func (gc *GuacamoleConnection) SetWebSocketWriter(writer io.Writer) error {
	// Guacamole连接通过内部流处理WebSocket数据
	// WebSocket数据通过Read/Write方法进行桥接
	return nil
}

func (gc *GuacamoleConnection) SetWebSocketReader(reader io.Reader) error {
	// Guacamole连接通过内部流处理WebSocket数据
	// WebSocket数据通过Read/Write方法进行桥接
	return nil
}

func (gc *GuacamoleConnection) GetMetadata() map[string]string {
	gc.mutex.RLock()
	defer gc.mutex.RUnlock()

	metadata := make(map[string]string)
	for k, v := range gc.metadata {
		metadata[k] = v
	}

	// 添加连接信息
	metadata["connection_id"] = gc.tunnel.ConnectionID
	metadata["bytes_read"] = fmt.Sprintf("%d", gc.bytesRead)
	metadata["bytes_written"] = fmt.Sprintf("%d", gc.bytesWritten)

	return metadata
}

func (gc *GuacamoleConnection) SetMetadata(key, value string) {
	gc.mutex.Lock()
	defer gc.mutex.Unlock()
	gc.metadata[key] = value
}

func (gc *GuacamoleConnection) updateLastActive() {
	gc.mutex.Lock()
	defer gc.mutex.Unlock()
	gc.lastActiveAt = time.Now()
}
