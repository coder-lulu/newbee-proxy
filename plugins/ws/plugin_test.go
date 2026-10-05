package ws

import (
	"context"
	"testing"
	"time"

    "github.com/coder-lulu/newbee-proxy/plugins/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWSPlugin(t *testing.T) {
	plugin := NewWSPlugin()

	assert.Equal(t, "websocket", plugin.Name())
	assert.Equal(t, "1.0.0", plugin.Version())
	assert.Equal(t, "stopped", plugin.status)
	assert.NotNil(t, plugin.connections)
	assert.NotNil(t, plugin.config)
	assert.Contains(t, plugin.SupportedProtocols(), "websocket")
}

func TestWSPlugin_Initialize(t *testing.T) {
	plugin := NewWSPlugin()

	config := map[string]interface{}{
		"max_connections": 100,
		"timeout":         30,
	}

	err := plugin.Initialize(config)
	require.NoError(t, err)

	assert.Equal(t, config, plugin.config)
	assert.NotNil(t, plugin.manager)
}

func TestWSPlugin_StartStop(t *testing.T) {
	plugin := NewWSPlugin()

	// 初始化插件
	err := plugin.Initialize(make(map[string]interface{}))
	require.NoError(t, err)

	// 测试启动
	err = plugin.Start()
	require.NoError(t, err)
	assert.True(t, plugin.IsRunning())
	assert.NotNil(t, plugin.startedAt)

	// 测试重复启动
	err = plugin.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// 测试停止
	err = plugin.Stop()
	require.NoError(t, err)
	assert.False(t, plugin.IsRunning())
	assert.NotNil(t, plugin.stoppedAt)

	// 测试重复停止
	err = plugin.Stop()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestWSPlugin_CreateConnection(t *testing.T) {
	plugin := NewWSPlugin()

	// 初始化并启动插件
	err := plugin.Initialize(make(map[string]interface{}))
	require.NoError(t, err)

	err = plugin.Start()
	require.NoError(t, err)
	defer plugin.Stop()

	// 创建连接
	ctx := context.Background()
	target := "ws://localhost:8080"
	credentials := &common.Credentials{
		Username: "test",
		Password: "password",
	}

	conn, err := plugin.CreateConnection(ctx, target, credentials)
	require.NoError(t, err)
	assert.NotNil(t, conn)

	// 检查连接是否添加到插件中
	connections := plugin.ListConnections()
	assert.Len(t, connections, 1)

	// 获取连接
	retrievedConn, exists := plugin.GetConnection(conn.ID())
	assert.True(t, exists)
	assert.Equal(t, conn.ID(), retrievedConn.ID())
}

func TestWSPlugin_CloseConnection(t *testing.T) {
	plugin := NewWSPlugin()

	// 初始化并启动插件
	err := plugin.Initialize(make(map[string]interface{}))
	require.NoError(t, err)

	err = plugin.Start()
	require.NoError(t, err)
	defer plugin.Stop()

	// 创建连接
	ctx := context.Background()
	target := "ws://localhost:8080"
	credentials := &common.Credentials{
		Username: "test",
		Password: "password",
	}

	conn, err := plugin.CreateConnection(ctx, target, credentials)
	require.NoError(t, err)

	// 关闭连接
	err = plugin.CloseConnection(conn.ID())
	require.NoError(t, err)

	// 确认连接已被移除
	connections := plugin.ListConnections()
	assert.Len(t, connections, 0)

	// 尝试获取已关闭的连接
	_, exists := plugin.GetConnection(conn.ID())
	assert.False(t, exists)
}

func TestWSPlugin_CreateConnectionWhenStopped(t *testing.T) {
	plugin := NewWSPlugin()

	// 在插件未启动时尝试创建连接
	ctx := context.Background()
	target := "ws://localhost:8080"
	credentials := &common.Credentials{
		Username: "test",
		Password: "password",
	}

	conn, err := plugin.CreateConnection(ctx, target, credentials)
	assert.Error(t, err)
	assert.Nil(t, conn)
	assert.Contains(t, err.Error(), "plugin not running")
}

func TestWSPlugin_GetMetrics(t *testing.T) {
	plugin := NewWSPlugin()

	// 初始化并启动插件
	err := plugin.Initialize(make(map[string]interface{}))
	require.NoError(t, err)

	err = plugin.Start()
	require.NoError(t, err)
	defer plugin.Stop()

	// 创建一些连接
	ctx := context.Background()
	target := "ws://localhost:8080"
	credentials := &common.Credentials{
		Username: "test",
		Password: "password",
	}

	conn1, err := plugin.CreateConnection(ctx, target, credentials)
	require.NoError(t, err)

	conn2, err := plugin.CreateConnection(ctx, target, credentials)
	require.NoError(t, err)

	// 获取指标
	metrics := plugin.GetMetrics()
	assert.NotNil(t, metrics)
	assert.Equal(t, 2, metrics.ActiveConnections)
	assert.NotZero(t, metrics.UpdatedAt)

	// 关闭一个连接
	err = plugin.CloseConnection(conn1.ID())
	require.NoError(t, err)

	// 再次获取指标
	metrics = plugin.GetMetrics()
	assert.Equal(t, 1, metrics.ActiveConnections)

	// 关闭剩余连接
	err = plugin.CloseConnection(conn2.ID())
	require.NoError(t, err)

	metrics = plugin.GetMetrics()
	assert.Equal(t, 0, metrics.ActiveConnections)
}

func TestWSPlugin_UpdateConfig(t *testing.T) {
	plugin := NewWSPlugin()

	// 初始配置
	initialConfig := map[string]interface{}{
		"max_connections": 50,
	}

	err := plugin.Initialize(initialConfig)
	require.NoError(t, err)

	// 更新配置
	newConfig := map[string]interface{}{
		"max_connections": 100,
		"timeout":         30,
	}

	err = plugin.UpdateConfig(newConfig)
	require.NoError(t, err)
	assert.Equal(t, newConfig, plugin.config)
}

func TestWSConnection_BasicFunctionality(t *testing.T) {
	conn := &WSConnection{
		id:           "test-conn-1",
		target:       "ws://localhost:8080",
		status:       common.StatusConnected,
		createdAt:    time.Now(),
		lastActiveAt: time.Now(),
		metadata:     make(map[string]string),
	}

	assert.Equal(t, "test-conn-1", conn.ID())
	assert.Equal(t, "ws://localhost:8080", conn.Target())
	assert.Equal(t, "websocket", conn.Protocol())
	assert.Equal(t, common.StatusConnected, conn.Status())
	assert.True(t, conn.IsConnected())
	assert.NotZero(t, conn.CreatedAt())
	assert.NotZero(t, conn.LastActiveAt())
}

func TestWSConnection_Metadata(t *testing.T) {
	conn := &WSConnection{
		id:       "test-conn-1",
		target:   "ws://localhost:8080",
		metadata: make(map[string]string),
	}

	// 设置元数据
	conn.SetMetadata("user", "testuser")
	conn.SetMetadata("session", "session123")

	metadata := conn.GetMetadata()
	assert.Equal(t, "testuser", metadata["user"])
	assert.Equal(t, "session123", metadata["session"])
}

func TestWSConnection_Close(t *testing.T) {
	conn := &WSConnection{
		id:       "test-conn-1",
		target:   "ws://localhost:8080",
		status:   common.StatusConnected,
		metadata: make(map[string]string),
	}

	err := conn.Close()
	require.NoError(t, err)
	assert.Equal(t, common.StatusDisconnected, conn.Status())
	assert.False(t, conn.IsConnected())
}
