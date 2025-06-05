package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"newbee-agent/internal/config"
	pb "newbee-agent/proto/agent"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// OpsClient OPS服务客户端
type OpsClient struct {
	config *config.Config
	conn   *grpc.ClientConn
	client pb.AgentServiceClient
	stream pb.AgentService_StreamClient
	logger logx.Logger

	// 流控制
	ctx        context.Context
	cancel     context.CancelFunc
	streamMu   sync.RWMutex
	connected  bool
	registered bool

	// 消息通道
	outgoing chan *pb.AgentMessage
	incoming chan *pb.OpsMessage

	// 状态管理
	agentToken    string
	lastHeartbeat time.Time

	// 停止信号
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewOpsClient 创建新的OPS客户端
func NewOpsClient(config *config.Config) *OpsClient {
	ctx, cancel := context.WithCancel(context.Background())

	return &OpsClient{
		config:     config,
		logger:     logx.WithContext(ctx),
		ctx:        ctx,
		cancel:     cancel,
		outgoing:   make(chan *pb.AgentMessage, 100),
		incoming:   make(chan *pb.OpsMessage, 100),
		stopCh:     make(chan struct{}),
		connected:  false,
		registered: false,
	}
}

// Start 启动OPS客户端
func (c *OpsClient) Start() error {
	if !c.config.OpsRpc.Enabled {
		c.logger.Info("OPS RPC client is disabled")
		return nil
	}

	c.logger.Info("Starting OPS client...")

	// 连接到OPS服务
	if err := c.connect(); err != nil {
		return fmt.Errorf("failed to connect to OPS: %w", err)
	}

	// 注册Agent
	if err := c.register(); err != nil {
		return fmt.Errorf("failed to register agent: %w", err)
	}

	// 建立双向流
	if err := c.startStream(); err != nil {
		return fmt.Errorf("failed to start stream: %w", err)
	}

	// 启动消息处理协程
	c.wg.Add(3)
	go c.handleOutgoingMessages()
	go c.handleIncomingMessages()
	go c.heartbeatLoop()

	c.logger.Info("OPS client started successfully")
	return nil
}

// Stop 停止OPS客户端
func (c *OpsClient) Stop() error {
	c.logger.Info("Stopping OPS client...")

	// 发送停止信号
	close(c.stopCh)

	// 取消上下文
	c.cancel()

	// 关闭流
	c.streamMu.Lock()
	if c.stream != nil {
		c.stream.CloseSend()
	}
	c.streamMu.Unlock()

	// 关闭连接
	if c.conn != nil {
		c.conn.Close()
	}

	// 等待协程结束
	c.wg.Wait()

	c.logger.Info("OPS client stopped")
	return nil
}

// connect 连接到OPS服务
func (c *OpsClient) connect() error {
	// 创建gRPC连接选项
	opts := []grpc.DialOption{
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(1024*1024*4), // 4MB
			grpc.MaxCallSendMsgSize(1024*1024*4), // 4MB
		),
	}

	// TLS配置
	if c.config.Security.EnableTLS {
		var creds credentials.TransportCredentials
		if c.config.Security.SkipVerify {
			creds = credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
		} else {
			creds = credentials.NewTLS(&tls.Config{})
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// 连接到第一个可用的endpoint
	var conn *grpc.ClientConn
	var err error

	for _, endpoint := range c.config.OpsRpc.Endpoints {
		c.logger.Infof("Attempting to connect to OPS at %s", endpoint)

		ctx, cancel := context.WithTimeout(c.ctx, time.Duration(c.config.OpsRpc.Timeout)*time.Millisecond)
		conn, err = grpc.DialContext(ctx, endpoint, opts...)
		cancel()

		if err == nil {
			// 检查连接状态
			if conn.GetState() == connectivity.Ready || conn.GetState() == connectivity.Idle {
				c.logger.Infof("Successfully connected to OPS at %s", endpoint)
				break
			}
			conn.Close()
		}

		c.logger.Errorf("Failed to connect to %s: %v", endpoint, err)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to any OPS endpoint: %w", err)
	}

	c.conn = conn
	c.client = pb.NewAgentServiceClient(conn)
	c.connected = true

	return nil
}

// register 注册Agent到OPS服务
func (c *OpsClient) register() error {
	c.logger.Info("Registering agent with OPS...")

	// 构建注册请求
	req := &pb.RegisterRequest{
		AgentId:      c.config.Agent.ID,
		Version:      c.config.Agent.Version,
		Region:       c.config.Agent.Region,
		Capabilities: c.config.Agent.Capabilities,
		AuthToken:    c.config.Agent.AuthToken,
		Resources: &pb.ResourceLimits{
			MaxConcurrentSessions: int32(c.config.Limits.MaxConcurrentSessions),
			MaxMemoryMb:           int32(c.config.Limits.MaxMemoryMB),
			MaxCpuPercent:         int32(c.config.Limits.MaxCPUPercent),
			SessionTimeoutSeconds: int32(c.config.Limits.SessionTimeoutSeconds),
		},
		NetworkInfo: &pb.NetworkInfo{
			LocalIp:         c.config.Network.LocalIP,
			PublicIp:        c.config.Network.PublicIP,
			NetworkSegments: c.config.Network.NetworkSegments,
		},
		Metadata: map[string]string{
			"start_time": time.Now().Format(time.RFC3339),
			"hostname":   getHostname(),
		},
	}

	// 发送注册请求
	ctx, cancel := context.WithTimeout(c.ctx, time.Duration(c.config.OpsRpc.Timeout)*time.Millisecond)
	defer cancel()

	resp, err := c.client.Register(ctx, req)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("registration rejected: %s", resp.Message)
	}

	// 保存OPS分配的令牌
	c.agentToken = resp.AgentToken
	c.registered = true

	c.logger.Infof("Agent registered successfully: %s", resp.Message)
	return nil
}

// startStream 启动双向流通信
func (c *OpsClient) startStream() error {
	c.logger.Info("Starting bidirectional stream...")

	c.streamMu.Lock()
	defer c.streamMu.Unlock()

	stream, err := c.client.Stream(c.ctx)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}

	c.stream = stream
	c.logger.Info("Bidirectional stream started")
	return nil
}

// handleOutgoingMessages 处理发出的消息
func (c *OpsClient) handleOutgoingMessages() {
	defer c.wg.Done()

	for {
		select {
		case msg := <-c.outgoing:
			c.streamMu.RLock()
			if c.stream != nil {
				if err := c.stream.Send(msg); err != nil {
					c.logger.Errorf("Failed to send message: %v", err)
				}
			}
			c.streamMu.RUnlock()

		case <-c.stopCh:
			return
		}
	}
}

// handleIncomingMessages 处理接收的消息
func (c *OpsClient) handleIncomingMessages() {
	defer c.wg.Done()

	for {
		select {
		case <-c.stopCh:
			return
		default:
			c.streamMu.RLock()
			stream := c.stream
			c.streamMu.RUnlock()

			if stream == nil {
				time.Sleep(time.Second)
				continue
			}

			msg, err := stream.Recv()
			if err != nil {
				c.logger.Errorf("Failed to receive message: %v", err)
				time.Sleep(time.Second)
				continue
			}

			// 将消息放入处理队列
			select {
			case c.incoming <- msg:
			default:
				c.logger.Error("Incoming message queue is full, dropping message")
			}
		}
	}
}

// heartbeatLoop 心跳循环
func (c *OpsClient) heartbeatLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(time.Duration(c.config.Heartbeat.Interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := c.sendHeartbeat(); err != nil {
				c.logger.Errorf("Failed to send heartbeat: %v", err)
			}

		case <-c.stopCh:
			return
		}
	}
}

// sendHeartbeat 发送心跳
func (c *OpsClient) sendHeartbeat() error {
	if !c.connected || !c.registered {
		return nil
	}

	// 构建心跳消息
	heartbeat := &pb.AgentMessage{
		AgentId:   c.config.Agent.ID,
		Timestamp: time.Now().Unix(),
		MessageId: generateMessageID(),
		Payload: &pb.AgentMessage_Heartbeat{
			Heartbeat: &pb.HeartbeatData{
				Status:          "online",
				ActiveSessions:  0, // TODO: 获取实际会话数
				CpuUsagePercent: getCurrentCPUUsage(),
				MemoryUsageMb:   getCurrentMemoryUsage(),
				UptimeSeconds:   int64(time.Since(time.Now()).Seconds()), // TODO: 获取实际运行时间
			},
		},
	}

	// 发送心跳
	select {
	case c.outgoing <- heartbeat:
		c.lastHeartbeat = time.Now()
		return nil
	default:
		return fmt.Errorf("outgoing queue is full")
	}
}

// SendMessage 发送消息到OPS
func (c *OpsClient) SendMessage(msg *pb.AgentMessage) error {
	if !c.connected || !c.registered {
		return fmt.Errorf("not connected or not registered")
	}

	select {
	case c.outgoing <- msg:
		return nil
	default:
		return fmt.Errorf("outgoing queue is full")
	}
}

// GetIncomingMessages 获取接收消息的通道
func (c *OpsClient) GetIncomingMessages() <-chan *pb.OpsMessage {
	return c.incoming
}

// IsConnected 检查是否已连接
func (c *OpsClient) IsConnected() bool {
	return c.connected && c.registered
}

// GetAgentToken 获取Agent令牌
func (c *OpsClient) GetAgentToken() string {
	return c.agentToken
}

// 辅助函数
func getHostname() string {
	// TODO: 实现获取主机名
	return "unknown"
}

func getCurrentCPUUsage() float32 {
	// TODO: 实现获取CPU使用率
	return 0.0
}

func getCurrentMemoryUsage() float32 {
	// TODO: 实现获取内存使用量
	return 0.0
}

func generateMessageID() string {
	// TODO: 实现生成消息ID
	return fmt.Sprintf("msg-%d", time.Now().UnixNano())
}
