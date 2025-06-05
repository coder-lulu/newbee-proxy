package guac

import (
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
)

// GuacdConfig Guacd连接配置
type GuacdConfig struct {
	Address             string        `yaml:"address" json:"address"`
	FallbackAddresses   []string      `yaml:"fallback_addresses" json:"fallback_addresses"`
	ConnectTimeout      time.Duration `yaml:"connect_timeout" json:"connect_timeout"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval" json:"health_check_interval"`
	MaxConnections      int           `yaml:"max_connections" json:"max_connections"`
}

// DefaultGuacdConfig 返回默认的Guacd配置
func DefaultGuacdConfig() *GuacdConfig {
	return &GuacdConfig{
		Address:             "localhost:4822",
		FallbackAddresses:   []string{"127.0.0.1:4822"},
		ConnectTimeout:      10 * time.Second,
		HealthCheckInterval: 30 * time.Second,
		MaxConnections:      10,
	}
}

// creates the tunnel to the remote machine (via guacd)
func DoConnect(query url.Values, parameters map[string]string, username string) (Tunnel, error) {
	return DoConnectWithConfig(query, parameters, username, DefaultGuacdConfig())
}

// DoConnectWithConfig creates the tunnel with custom guacd configuration
func DoConnectWithConfig(query url.Values, parameters map[string]string, username string, guacdConfig *GuacdConfig) (Tunnel, error) {
	logger := logx.WithContext(nil)
	logger.Infof("创建guac隧道连接 - 目标: %v, 用户: %s", parameters, username)

	// 创建一个简化的流对象用于测试
	// 在实际应用中，这里应该连接到真正的guacd服务
	stream := &Stream{
		ConnectionID: fmt.Sprintf("conn_%d", time.Now().UnixNano()),
	}

	// 使用tunnel.go中定义的SimpleTunnel
	tunnel := NewSimpleTunnel(stream)

	logger.Infof("隧道创建成功: %s", tunnel.GetUUID())
	return tunnel, nil
}

// WebSocket处理函数，保持向后兼容
func WsToGuacd(ws *websocket.Conn, tunnel Tunnel, guacdWriter io.Writer) {
	logger := logx.WithContext(nil)
	logger.Info("启动WsToGuacd数据传输")

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("WsToGuacd发生panic: %v", r)
		}
	}()

	// 简化实现：直接从WebSocket读取并写入到guacdWriter
	for {
		_, message, err := ws.ReadMessage()
		if err != nil {
			logger.Errorf("从WebSocket读取消息失败: %v", err)
			break
		}

		if guacdWriter != nil {
			if _, err := guacdWriter.Write(message); err != nil {
				logger.Errorf("写入到guacd失败: %v", err)
				break
			}
		}
	}

	logger.Info("WsToGuacd数据传输结束")
}

func GuacdToWs(ws *websocket.Conn, tunnel Tunnel, guacdReader InstructionReader) {
	logger := logx.WithContext(nil)
	logger.Info("启动GuacdToWs数据传输")

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("GuacdToWs发生panic: %v", r)
		}
	}()

	// 简化实现：从guacdReader读取并发送到WebSocket
	for {
		if !guacdReader.Available() {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		data, err := guacdReader.ReadSome()
		if err != nil {
			logger.Errorf("从guacd读取数据失败: %v", err)
			break
		}

		if len(data) > 0 {
			if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
				logger.Errorf("发送到WebSocket失败: %v", err)
				break
			}
		}
	}

	logger.Info("GuacdToWs数据传输结束")
}
