package client

// import (
// 	"context"
// 	"crypto/tls"
// 	"encoding/json"
// 	"fmt"
// 	"runtime"
// 	"sync"
// 	"time"

// 	"newbee-agent/internal/config"

// 	"github.com/zeromicro/go-zero/core/logx"
// 	"google.golang.org/grpc"
// 	"google.golang.org/grpc/connectivity"
// 	"google.golang.org/grpc/credentials"
// 	"google.golang.org/grpc/credentials/insecure"
// 	"google.golang.org/grpc/keepalive"
// )

// // OpsStreamClient 新的流式OPS客户端
// type OpsStreamClient struct {
// 	config *config.Config
// 	conn   *grpc.ClientConn
// 	client OpsClient             // 这里需要使用生成的gRPC客户端接口
// 	stream Ops_AgentStreamClient // 这里需要使用生成的流式接口
// 	logger logx.Logger

// 	// 流控制
// 	ctx        context.Context
// 	cancel     context.CancelFunc
// 	streamMu   sync.RWMutex
// 	connected  bool
// 	registered bool

// 	// 消息通道
// 	outgoing chan *AgentStreamMessage
// 	incoming chan *AgentStreamMessage

// 	// 状态管理
// 	agentID           string
// 	agentToken        string
// 	lastHeartbeat     time.Time
// 	heartbeatInterval time.Duration

// 	// 任务处理
// 	taskHandler TaskHandler

// 	// 停止信号
// 	stopCh chan struct{}
// 	wg     sync.WaitGroup
// }

// // 任务处理器接口
// type TaskHandler interface {
// 	HandleTask(taskData map[string]interface{}) error
// }

// // Agent流式消息（临时定义，实际应该从生成的代码导入）
// type AgentStreamMessage struct {
// 	MessageId   string `json:"message_id"`
// 	AgentId     string `json:"agent_id"`
// 	MessageType string `json:"message_type"`
// 	Timestamp   int64  `json:"timestamp"`
// 	Payload     string `json:"payload"`
// }

// // Agent注册请求（临时定义）
// type RegisterAgentRequest struct {
// 	AgentId      string   `json:"agent_id"`
// 	AgentName    string   `json:"agent_name"`
// 	Version      string   `json:"version"`
// 	Capabilities []string `json:"capabilities"`
// 	SystemInfo   string   `json:"system_info"`
// 	Config       string   `json:"config"`
// 	Timestamp    int64    `json:"timestamp"`
// }

// // Agent注册响应（临时定义）
// type RegisterAgentResponse struct {
// 	Success           bool   `json:"success"`
// 	Message           string `json:"message"`
// 	AssignedRegion    string `json:"assigned_region"`
// 	HeartbeatInterval int64  `json:"heartbeat_interval"`
// 	TaskPollInterval  int64  `json:"task_poll_interval"`
// 	ConfigUpdates     string `json:"config_updates"`
// 	Timestamp         int64  `json:"timestamp"`
// }

// // 心跳请求（临时定义）
// type HeartbeatRequest struct {
// 	AgentId        string `json:"agent_id"`
// 	Status         string `json:"status"`
// 	Uptime         int64  `json:"uptime"`
// 	ActiveSessions int32  `json:"active_sessions"`
// 	ActiveTasks    int32  `json:"active_tasks"`
// 	ResourceUsage  string `json:"resource_usage"`
// 	PluginStatus   string `json:"plugin_status"`
// 	Timestamp      int64  `json:"timestamp"`
// }

// // 心跳响应（临时定义）
// type HeartbeatResponse struct {
// 	Status        string `json:"status"`
// 	NextHeartbeat int64  `json:"next_heartbeat"`
// 	PendingTasks  int32  `json:"pending_tasks"`
// 	ConfigVersion string `json:"config_version"`
// 	Commands      string `json:"commands"`
// 	Timestamp     int64  `json:"timestamp"`
// }

// // 临时接口定义
// type OpsClient interface {
// 	RegisterAgent(ctx context.Context, in *RegisterAgentRequest) (*RegisterAgentResponse, error)
// 	Heartbeat(ctx context.Context, in *HeartbeatRequest) (*HeartbeatResponse, error)
// 	AgentStream(ctx context.Context) (Ops_AgentStreamClient, error)
// }

// type Ops_AgentStreamClient interface {
// 	Send(*AgentStreamMessage) error
// 	Recv() (*AgentStreamMessage, error)
// 	CloseSend() error
// }

// // NewOpsStreamClient 创建新的流式OPS客户端
// func NewOpsStreamClient(config *config.Config, taskHandler TaskHandler) *OpsStreamClient {
// 	ctx, cancel := context.WithCancel(context.Background())

// 	return &OpsStreamClient{
// 		config:            config,
// 		logger:            logx.WithContext(ctx),
// 		ctx:               ctx,
// 		cancel:            cancel,
// 		outgoing:          make(chan *AgentStreamMessage, 100),
// 		incoming:          make(chan *AgentStreamMessage, 100),
// 		stopCh:            make(chan struct{}),
// 		connected:         false,
// 		registered:        false,
// 		agentID:           config.Agent.ID,
// 		heartbeatInterval: 30 * time.Second, // 默认30秒
// 		taskHandler:       taskHandler,
// 	}
// }

// // Start 启动流式OPS客户端
// func (c *OpsStreamClient) Start() error {
// 	if !c.config.OpsRpc.Enabled {
// 		c.logger.Info("OPS RPC client is disabled")
// 		return nil
// 	}

// 	c.logger.Info("Starting OPS stream client...")

// 	// 连接到OPS服务
// 	if err := c.connect(); err != nil {
// 		return fmt.Errorf("failed to connect to OPS: %w", err)
// 	}

// 	// 注册Agent
// 	if err := c.register(); err != nil {
// 		return fmt.Errorf("failed to register agent: %w", err)
// 	}

// 	// 建立双向流
// 	if err := c.startStream(); err != nil {
// 		return fmt.Errorf("failed to start stream: %w", err)
// 	}

// 	// 启动消息处理协程
// 	c.wg.Add(4)
// 	go c.handleOutgoingMessages()
// 	go c.handleIncomingMessages()
// 	go c.heartbeatLoop()
// 	go c.streamReceiveLoop()

// 	c.logger.Info("OPS stream client started successfully")
// 	return nil
// }

// // Stop 停止流式OPS客户端
// func (c *OpsStreamClient) Stop() error {
// 	c.logger.Info("Stopping OPS stream client...")

// 	// 发送停止信号
// 	close(c.stopCh)

// 	// 取消上下文
// 	c.cancel()

// 	// 关闭流
// 	c.streamMu.Lock()
// 	if c.stream != nil {
// 		c.stream.CloseSend()
// 	}
// 	c.streamMu.Unlock()

// 	// 关闭连接
// 	if c.conn != nil {
// 		c.conn.Close()
// 	}

// 	// 等待协程结束
// 	c.wg.Wait()

// 	c.logger.Info("OPS stream client stopped")
// 	return nil
// }

// // connect 连接到OPS服务
// func (c *OpsStreamClient) connect() error {
// 	// 创建gRPC连接选项
// 	opts := []grpc.DialOption{
// 		grpc.WithKeepaliveParams(keepalive.ClientParameters{
// 			Time:                10 * time.Second,
// 			Timeout:             3 * time.Second,
// 			PermitWithoutStream: true,
// 		}),
// 		grpc.WithDefaultCallOptions(
// 			grpc.MaxCallRecvMsgSize(1024*1024*4), // 4MB
// 			grpc.MaxCallSendMsgSize(1024*1024*4), // 4MB
// 		),
// 	}

// 	// TLS配置
// 	if c.config.Security.EnableTLS {
// 		var creds credentials.TransportCredentials
// 		if c.config.Security.SkipVerify {
// 			creds = credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
// 		} else {
// 			creds = credentials.NewTLS(&tls.Config{})
// 		}
// 		opts = append(opts, grpc.WithTransportCredentials(creds))
// 	} else {
// 		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	}

// 	// 连接到第一个可用的endpoint
// 	var conn *grpc.ClientConn
// 	var err error

// 	for _, endpoint := range c.config.OpsRpc.Endpoints {
// 		c.logger.Infof("Attempting to connect to OPS at %s", endpoint)

// 		conn, err = grpc.NewClient(endpoint, opts...)

// 		if err == nil {
// 			// 检查连接状态
// 			if conn.GetState() == connectivity.Ready || conn.GetState() == connectivity.Idle {
// 				c.logger.Infof("Successfully connected to OPS at %s", endpoint)
// 				break
// 			}
// 			conn.Close()
// 		}

// 		c.logger.Errorf("Failed to connect to %s: %v", endpoint, err)
// 	}

// 	if err != nil {
// 		return fmt.Errorf("failed to connect to any OPS endpoint: %w", err)
// 	}

// 	c.conn = conn
// 	// c.client = NewOpsClient(conn) // 实际需要使用生成的客户端
// 	c.connected = true

// 	return nil
// }

// // register 注册Agent到OPS服务
// func (c *OpsStreamClient) register() error {
// 	c.logger.Info("Registering agent with OPS...")

// 	// 构建系统信息
// 	systemInfo := map[string]interface{}{
// 		"os":         runtime.GOOS,
// 		"arch":       runtime.GOARCH,
// 		"hostname":   c.config.Agent.Name,
// 		"ip_address": "127.0.0.1", // 实际应该获取真实IP
// 		"cpu_cores":  runtime.NumCPU(),
// 		"memory_gb":  8, // 实际应该获取真实内存
// 	}
// 	systemInfoJSON, _ := json.Marshal(systemInfo)

// 	// 构建配置信息
// 	agentConfig := map[string]interface{}{
// 		"max_concurrent_sessions": 50,
// 		"max_concurrent_tasks":    10,
// 		"supported_protocols":     c.config.Agent.Capabilities,
// 		"log_level":               "info",
// 	}
// 	configJSON, _ := json.Marshal(agentConfig)

// 	// 构建注册请求
// 	req := &RegisterAgentRequest{
// 		AgentId:      c.config.Agent.ID,
// 		AgentName:    c.config.Agent.Name,
// 		Version:      c.config.Agent.Version,
// 		Capabilities: c.config.Agent.Capabilities,
// 		SystemInfo:   string(systemInfoJSON),
// 		Config:       string(configJSON),
// 		Timestamp:    time.Now().Unix(),
// 	}

// 	// 发送注册请求
// 	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
// 	defer cancel()

// 	resp, err := c.client.RegisterAgent(ctx, req)
// 	if err != nil {
// 		return fmt.Errorf("registration failed: %w", err)
// 	}

// 	if !resp.Success {
// 		return fmt.Errorf("registration rejected: %s", resp.Message)
// 	}

// 	// 更新配置
// 	c.heartbeatInterval = time.Duration(resp.HeartbeatInterval) * time.Second
// 	c.registered = true

// 	c.logger.Infof("Agent registered successfully: Region=%s, HeartbeatInterval=%v",
// 		resp.AssignedRegion, c.heartbeatInterval)

// 	return nil
// }

// // startStream 建立双向流
// func (c *OpsStreamClient) startStream() error {
// 	c.logger.Info("Starting bidirectional stream...")

// 	stream, err := c.client.AgentStream(c.ctx)
// 	if err != nil {
// 		return fmt.Errorf("failed to create stream: %w", err)
// 	}

// 	c.streamMu.Lock()
// 	c.stream = stream
// 	c.streamMu.Unlock()

// 	c.logger.Info("Bidirectional stream established")
// 	return nil
// }

// // streamReceiveLoop 流接收循环
// func (c *OpsStreamClient) streamReceiveLoop() {
// 	defer c.wg.Done()

// 	for {
// 		select {
// 		case <-c.stopCh:
// 			return
// 		default:
// 			c.streamMu.RLock()
// 			stream := c.stream
// 			c.streamMu.RUnlock()

// 			if stream == nil {
// 				time.Sleep(time.Second)
// 				continue
// 			}

// 			msg, err := stream.Recv()
// 			if err != nil {
// 				c.logger.Errorf("Stream receive error: %v", err)
// 				time.Sleep(5 * time.Second)
// 				continue
// 			}

// 			// 处理接收到的消息
// 			select {
// 			case c.incoming <- msg:
// 			default:
// 				c.logger.Warn("Incoming message buffer full, dropping message")
// 			}
// 		}
// 	}
// }

// // handleOutgoingMessages 处理发送消息
// func (c *OpsStreamClient) handleOutgoingMessages() {
// 	defer c.wg.Done()

// 	for {
// 		select {
// 		case <-c.stopCh:
// 			return
// 		case msg := <-c.outgoing:
// 			c.streamMu.RLock()
// 			stream := c.stream
// 			c.streamMu.RUnlock()

// 			if stream == nil {
// 				c.logger.Warn("Stream not available, dropping outgoing message")
// 				continue
// 			}

// 			if err := stream.Send(msg); err != nil {
// 				c.logger.Errorf("Failed to send message: %v", err)
// 			}
// 		}
// 	}
// }

// // handleIncomingMessages 处理接收消息
// func (c *OpsStreamClient) handleIncomingMessages() {
// 	defer c.wg.Done()

// 	for {
// 		select {
// 		case <-c.stopCh:
// 			return
// 		case msg := <-c.incoming:
// 			if err := c.processMessage(msg); err != nil {
// 				c.logger.Errorf("Failed to process message: %v", err)
// 			}
// 		}
// 	}
// }

// // processMessage 处理收到的消息
// func (c *OpsStreamClient) processMessage(msg *AgentStreamMessage) error {
// 	c.logger.Debugf("Processing message: Type=%s, AgentID=%s", msg.MessageType, msg.AgentId)

// 	switch msg.MessageType {
// 	case "task_assignment":
// 		return c.handleTaskAssignment(msg)
// 	case "config_update":
// 		return c.handleConfigUpdate(msg)
// 	case "health_check":
// 		return c.handleHealthCheck(msg)
// 	case "shutdown":
// 		return c.handleShutdown(msg)
// 	default:
// 		c.logger.Infof("Unknown message type: %s", msg.MessageType)
// 		return nil
// 	}
// }

// // handleTaskAssignment 处理任务分配
// func (c *OpsStreamClient) handleTaskAssignment(msg *AgentStreamMessage) error {
// 	c.logger.Infof("Received task assignment: %s", msg.MessageId)

// 	var taskData map[string]interface{}
// 	if err := json.Unmarshal([]byte(msg.Payload), &taskData); err != nil {
// 		return fmt.Errorf("failed to parse task assignment: %w", err)
// 	}

// 	// 发送任务接受确认
// 	c.sendTaskAcceptance(msg.MessageId)

// 	// 执行任务
// 	go func() {
// 		if err := c.taskHandler.HandleTask(taskData); err != nil {
// 			c.logger.Errorf("Task execution failed: %v", err)
// 			c.sendTaskResult(msg.MessageId, false, err.Error())
// 		} else {
// 			c.sendTaskResult(msg.MessageId, true, "Task completed successfully")
// 		}
// 	}()

// 	return nil
// }

// // handleConfigUpdate 处理配置更新
// func (c *OpsStreamClient) handleConfigUpdate(msg *AgentStreamMessage) error {
// 	c.logger.Infof("Received config update: %s", msg.MessageId)

// 	var configUpdate map[string]interface{}
// 	if err := json.Unmarshal([]byte(msg.Payload), &configUpdate); err != nil {
// 		return fmt.Errorf("failed to parse config update: %w", err)
// 	}

// 	// TODO: 实际的配置更新逻辑
// 	c.logger.Infof("Config update applied: %v", configUpdate)

// 	return nil
// }

// // handleHealthCheck 处理健康检查
// func (c *OpsStreamClient) handleHealthCheck(msg *AgentStreamMessage) error {
// 	c.logger.Debugf("Received health check: %s", msg.MessageId)

// 	// 响应健康检查
// 	response := map[string]interface{}{
// 		"status":    "healthy",
// 		"timestamp": time.Now().Unix(),
// 		"uptime":    time.Since(time.Now()).Seconds(), // 实际应该计算真实的运行时间
// 	}

// 	return c.sendMessage("health_check_response", response)
// }

// // handleShutdown 处理关机请求
// func (c *OpsStreamClient) handleShutdown(msg *AgentStreamMessage) error {
// 	c.logger.Infof("Received shutdown request: %s", msg.MessageId)

// 	var shutdownReq map[string]interface{}
// 	if err := json.Unmarshal([]byte(msg.Payload), &shutdownReq); err != nil {
// 		return fmt.Errorf("failed to parse shutdown request: %w", err)
// 	}

// 	// TODO: 实际的关机逻辑
// 	gracePeriod := 30 // 默认30秒
// 	if gp, exists := shutdownReq["grace_period"]; exists {
// 		if gpInt, ok := gp.(float64); ok {
// 			gracePeriod = int(gpInt)
// 		}
// 	}

// 	c.logger.Infof("Initiating graceful shutdown with %d seconds grace period", gracePeriod)

// 	// 启动优雅关机
// 	go func() {
// 		time.Sleep(time.Duration(gracePeriod) * time.Second)
// 		c.Stop()
// 	}()

// 	return nil
// }

// // heartbeatLoop 心跳循环
// func (c *OpsStreamClient) heartbeatLoop() {
// 	defer c.wg.Done()

// 	ticker := time.NewTicker(c.heartbeatInterval)
// 	defer ticker.Stop()

// 	for {
// 		select {
// 		case <-c.stopCh:
// 			return
// 		case <-ticker.C:
// 			if err := c.sendHeartbeat(); err != nil {
// 				c.logger.Errorf("Failed to send heartbeat: %v", err)
// 			}
// 		}
// 	}
// }

// // sendHeartbeat 发送心跳
// func (c *OpsStreamClient) sendHeartbeat() error {
// 	heartbeat := map[string]interface{}{
// 		"status":          "running",
// 		"uptime":          time.Since(time.Now()).Seconds(), // 实际应该计算真实的运行时间
// 		"active_sessions": 0,                                // 实际应该获取真实的会话数
// 		"active_tasks":    0,                                // 实际应该获取真实的任务数
// 		"resource_usage": map[string]interface{}{
// 			"cpu_percent":    getCurrentCPUUsage(),
// 			"memory_percent": getCurrentMemoryUsage(),
// 		},
// 		"plugin_status": map[string]interface{}{
// 			"ssh":    map[string]interface{}{"status": "running", "connections": 0},
// 			"rdp":    map[string]interface{}{"status": "running", "connections": 0},
// 			"telnet": map[string]interface{}{"status": "running", "connections": 0},
// 		},
// 	}

// 	return c.sendMessage("heartbeat", heartbeat)
// }

// // sendTaskAcceptance 发送任务接受确认
// func (c *OpsStreamClient) sendTaskAcceptance(taskID string) {
// 	acceptance := map[string]interface{}{
// 		"task_id":   taskID,
// 		"status":    "accepted",
// 		"timestamp": time.Now().Unix(),
// 	}

// 	c.sendMessage("task_status", acceptance)
// }

// // sendTaskResult 发送任务结果
// func (c *OpsStreamClient) sendTaskResult(taskID string, success bool, message string) {
// 	result := map[string]interface{}{
// 		"task_id":   taskID,
// 		"success":   success,
// 		"message":   message,
// 		"timestamp": time.Now().Unix(),
// 	}

// 	c.sendMessage("task_result", result)
// }

// // sendMessage 发送消息
// func (c *OpsStreamClient) sendMessage(messageType string, payload interface{}) error {
// 	payloadJSON, err := json.Marshal(payload)
// 	if err != nil {
// 		return fmt.Errorf("failed to marshal payload: %w", err)
// 	}

// 	msg := &AgentStreamMessage{
// 		MessageId:   generateMessageID(),
// 		AgentId:     c.agentID,
// 		MessageType: messageType,
// 		Timestamp:   time.Now().Unix(),
// 		Payload:     string(payloadJSON),
// 	}

// 	select {
// 	case c.outgoing <- msg:
// 		return nil
// 	default:
// 		return fmt.Errorf("outgoing message buffer full")
// 	}
// }

// // IsConnected 检查是否已连接
// func (c *OpsStreamClient) IsConnected() bool {
// 	return c.connected && c.registered
// }

// // getCurrentCPUUsage 获取当前CPU使用率
// func getCurrentCPUUsage() float32 {
// 	// TODO: 实现真实的CPU使用率获取
// 	return 25.0
// }

// // getCurrentMemoryUsage 获取当前内存使用率
// func getCurrentMemoryUsage() float32 {
// 	// TODO: 实现真实的内存使用率获取
// 	return 45.0
// }

// // generateMessageID 生成消息ID
// func generateMessageID() string {
// 	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
// }
