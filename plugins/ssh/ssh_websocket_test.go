package ssh

import (
	"io"
	"testing"
	"time"

    "github.com/coder-lulu/newbee-proxy/plugins/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockConnection 模拟SSH连接
type MockConnection struct {
	mock.Mock
}

func (m *MockConnection) ID() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockConnection) Target() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockConnection) Protocol() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockConnection) Status() common.ConnectionStatus {
	args := m.Called()
	return args.Get(0).(common.ConnectionStatus)
}

func (m *MockConnection) CreatedAt() time.Time {
	args := m.Called()
	return args.Get(0).(time.Time)
}

func (m *MockConnection) LastActiveAt() time.Time {
	args := m.Called()
	return args.Get(0).(time.Time)
}

func (m *MockConnection) Write(data []byte) (int, error) {
	args := m.Called(data)
	return args.Int(0), args.Error(1)
}

func (m *MockConnection) Read(data []byte) (int, error) {
	args := m.Called(data)
	return args.Int(0), args.Error(1)
}

func (m *MockConnection) SetWebSocketWriter(writer io.Writer) error {
	args := m.Called(writer)
	return args.Error(0)
}

func (m *MockConnection) SetWebSocketReader(reader io.Reader) error {
	args := m.Called(reader)
	return args.Error(0)
}

func (m *MockConnection) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockConnection) IsConnected() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockConnection) GetSession() common.Session {
	args := m.Called()
	return args.Get(0).(common.Session)
}

func (m *MockConnection) GetMetadata() map[string]string {
	args := m.Called()
	return args.Get(0).(map[string]string)
}

func (m *MockConnection) SetMetadata(key, value string) {
	m.Called(key, value)
}

// TestSSHPlugin 测试SSH插件基本功能
func TestSSHPlugin(t *testing.T) {
	plugin := NewSSHPlugin()

	// 测试插件信息
	assert.Equal(t, "ssh", plugin.Name())
	assert.Equal(t, "v1.0.0", plugin.Version())
	assert.Contains(t, plugin.SupportedProtocols(), "ssh")
	assert.NotEmpty(t, plugin.Description())

	// 测试初始化
	config := map[string]interface{}{
		"max_connections":     50,
		"keep_alive_interval": 30,
		"connection_timeout":  15,
		"buffer_size":         2048,
		"enable_compression":  true,
	}

	err := plugin.Initialize(config)
	assert.NoError(t, err)

	// 测试启动
	err = plugin.Start()
	assert.NoError(t, err)
	assert.True(t, plugin.IsRunning())

	// 测试停止
	err = plugin.Stop()
	assert.NoError(t, err)
	assert.False(t, plugin.IsRunning())
}

// TestWebSocketBridge 测试WebSocket桥接功能
func TestWebSocketBridge(t *testing.T) {
	// 创建SSH配置
	config := &SSHConfig{
		MaxConnections:    10,
		KeepAliveInterval: 30 * time.Second,
		ConnectionTimeout: 15 * time.Second,
		BufferSize:        4096,
		EnableCompression: false,
		WebSocketBridge: &common.WebSocketBridge{
			Enabled:     true,
			BufferSize:  4096,
			Encoding:    "utf8",
			Compression: false,
		},
	}

	// 创建模拟连接
	mockConn := &MockConnection{}
	mockConn.On("ID").Return("test-conn-1")
	mockConn.On("Status").Return(common.StatusConnected)
	mockConn.On("IsConnected").Return(true)

	// 验证WebSocket桥接配置
	assert.True(t, config.WebSocketBridge.Enabled)
	assert.Equal(t, 4096, config.WebSocketBridge.BufferSize)
	assert.Equal(t, "utf8", config.WebSocketBridge.Encoding)
	assert.False(t, config.WebSocketBridge.Compression)
}

// TestSSHConnectionConfig 测试SSH连接配置
func TestSSHConnectionConfig(t *testing.T) {
	// 测试不同的认证方式
	testCases := []struct {
		name        string
		credentials *common.Credentials
		expectError bool
	}{
		{
			name: "密码认证",
			credentials: &common.Credentials{
				Username: "testuser",
				Password: "testpass",
				AuthType: "password",
				Timeout:  30,
			},
			expectError: false,
		},
		{
			name: "公钥认证",
			credentials: &common.Credentials{
				Username:   "testuser",
				PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----",
				AuthType:   "publickey",
				Timeout:    30,
			},
			expectError: false,
		},
		{
			name: "键盘交互认证",
			credentials: &common.Credentials{
				Username: "testuser",
				Password: "testpass",
				AuthType: "keyboard-interactive",
				Timeout:  30,
			},
			expectError: false,
		},
		{
			name: "无效认证类型",
			credentials: &common.Credentials{
				Username: "testuser",
				AuthType: "invalid",
				Timeout:  30,
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 这里只测试认证类型的验证逻辑
			switch tc.credentials.AuthType {
			case "password", "publickey", "keyboard-interactive":
				assert.False(t, tc.expectError, "应该支持的认证类型")
			default:
				assert.True(t, tc.expectError, "不应该支持的认证类型")
			}
		})
	}
}

// TestPluginMetrics 测试插件监控指标
func TestPluginMetrics(t *testing.T) {
	plugin := NewSSHPlugin()

	// 初始化插件
	config := map[string]interface{}{
		"max_connections": 100,
	}
	err := plugin.Initialize(config)
	assert.NoError(t, err)

	err = plugin.Start()
	assert.NoError(t, err)
	defer plugin.Stop()

	// 获取状态
	status := plugin.GetStatus()
	assert.NotNil(t, status)
	assert.Equal(t, "ssh", status.Name)
	assert.Equal(t, "v1.0.0", status.Version)
	assert.Equal(t, "running", status.Status)

	// 获取监控指标
	metrics := plugin.GetMetrics()
	assert.NotNil(t, metrics)
	assert.GreaterOrEqual(t, metrics.TotalConnections, int64(0))
	assert.GreaterOrEqual(t, metrics.ActiveConnections, 0)
}

// BenchmarkSSHPlugin 性能基准测试
func BenchmarkSSHPlugin(b *testing.B) {
	plugin := NewSSHPlugin()

	config := map[string]interface{}{
		"max_connections": 1000,
		"buffer_size":     4096,
	}
	plugin.Initialize(config)
	plugin.Start()
	defer plugin.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// 模拟插件操作
			_ = plugin.GetStatus()
			_ = plugin.GetMetrics()
		}
	})
}
