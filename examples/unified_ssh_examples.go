package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// UnifiedSSHExamples 统一SSH使用示例
func UnifiedSSHExamples() {
	fmt.Println("🚀 统一SSH架构使用示例")
	fmt.Println("=========================")

	// 示例1: 直接SSH连接
	fmt.Println("\n📡 示例1: 直接SSH连接")
	directSSHExample()

	// 示例2: 单跳板机连接
	fmt.Println("\n🔧 示例2: 单跳板机SSH连接")
	singleJumpHostExample()

	// 示例3: 多跳板机连接
	fmt.Println("\n🖥️ 示例3: 多跳板机SSH连接")
	multiJumpHostExample()

	// 示例4: 通过guacd的SSH连接
	fmt.Println("\n📞 示例4: 通过guacd的SSH连接")
	guacdSSHExample()

	// 示例5: WebSocket连接展示
	websocketConnectionExample()

	fmt.Println("\n✅ 所有示例展示完成！")
}

// 直接SSH连接示例
func directSSHExample() {
	request := SSHTunnelRequest{
		Target:    "192.168.1.100",
		Port:      22,
		Username:  "root",
		Password:  "password",
		Cols:      80,
		Rows:      24,
		SessionID: "direct-ssh-001",
		Method:    "direct", // 强制使用直接连接
	}

	fmt.Printf("连接配置: %+v\n", request)

	// 模拟WebSocket连接和响应
	response := SSHTunnelResponse{
		Success:      true,
		Message:      "SSH tunnel established successfully",
		SessionID:    request.SessionID,
		ConnectionID: "ssh-192.168.1.100-direct-ssh-001",
		Target:       "192.168.1.100:22",
		Timestamp:    time.Now(),
		Method:       "direct",
	}

	printResponse(response)
}

// 单跳板机连接示例
func singleJumpHostExample() {
	request := SSHTunnelRequest{
		Target:    "192.168.2.100",
		Port:      22,
		Username:  "user",
		Password:  "userpass",
		Cols:      120,
		Rows:      30,
		SessionID: "jump-ssh-001",
		Method:    "auto", // 自动选择连接方式
		JumpHosts: []SSHJumpHost{
			{
				Hostname:   "jumphost.example.com",
				Port:       22,
				Username:   "jumpuser",
				Password:   "jumppass",
				AuthMethod: "password",
			},
		},
		EnableRecording: false,
	}

	fmt.Printf("连接配置: %+v\n", request)

	response := SSHTunnelResponse{
		Success:      true,
		Message:      "SSH tunnel established successfully",
		SessionID:    request.SessionID,
		ConnectionID: "ssh-192.168.2.100-jump-ssh-001",
		Target:       "192.168.2.100:22",
		JumpHosts: []map[string]interface{}{
			{
				"hostname":    "jumphost.example.com",
				"port":        "22",
				"username":    "jumpuser",
				"auth_method": "password",
			},
		},
		Timestamp: time.Now(),
		Method:    "direct", // 复杂跳板机使用直接连接
	}

	printResponse(response)
}

// 多跳板机连接示例
func multiJumpHostExample() {
	request := SSHTunnelRequest{
		Target:    "192.168.3.100",
		Port:      22,
		Username:  "admin",
		Password:  "adminpass",
		Cols:      100,
		Rows:      40,
		SessionID: "multi-jump-ssh-001",
		Method:    "direct", // 多跳板机强制使用直接连接
		JumpHosts: []SSHJumpHost{
			{
				Hostname:   "jump1.example.com",
				Port:       22,
				Username:   "jump1user",
				Password:   "jump1pass",
				AuthMethod: "password",
			},
			{
				Hostname:   "jump2.example.com",
				Port:       22,
				Username:   "jump2user",
				PrivateKey: "-----BEGIN RSA PRIVATE KEY-----\n...",
				AuthMethod: "private_key",
			},
		},
		EnableRecording: true,
		Options: map[string]string{
			"terminal_type": "xterm-256color",
			"font_size":     "14",
		},
	}

	fmt.Printf("连接配置: %+v\n", request)

	response := SSHTunnelResponse{
		Success:      true,
		Message:      "SSH tunnel established successfully",
		SessionID:    request.SessionID,
		ConnectionID: "ssh-192.168.3.100-multi-jump-ssh-001",
		Target:       "192.168.3.100:22",
		JumpHosts: []map[string]interface{}{
			{
				"hostname":    "jump1.example.com",
				"port":        "22",
				"username":    "jump1user",
				"auth_method": "password",
			},
			{
				"hostname":    "jump2.example.com",
				"port":        "22",
				"username":    "jump2user",
				"auth_method": "private_key",
			},
		},
		Timestamp: time.Now(),
		Method:    "direct",
	}

	printResponse(response)
}

// 通过guacd的SSH连接示例
func guacdSSHExample() {
	request := SSHTunnelRequest{
		Target:          "192.168.1.200",
		Port:            22,
		Username:        "developer",
		Password:        "devpass",
		Cols:            80,
		Rows:            24,
		SessionID:       "guacd-ssh-001",
		Method:          "guacd", // 强制使用guacd
		EnableRecording: true,
		Options: map[string]string{
			"color_scheme":   "green-black",
			"font_name":      "monospace",
			"enable_sftp":    "true",
			"recording_path": "/tmp/ssh-sessions",
		},
	}

	fmt.Printf("连接配置: %+v\n", request)

	response := SSHTunnelResponse{
		Success:      true,
		Message:      "SSH tunnel established successfully",
		SessionID:    request.SessionID,
		ConnectionID: "tunnel-uuid-12345",
		Target:       "guacd:connection-id-67890",
		Timestamp:    time.Now(),
		Method:       "guacd",
	}

	printResponse(response)
}

// WebSocket连接示例
func websocketConnectionExample() {
	fmt.Println("\n🔌 WebSocket连接示例")

	// 统一SSH路由
	fmt.Println("统一SSH连接路由:")
	fmt.Println("  ws://localhost:8080/ws/ssh/unified")

	// guacd SSH路由
	fmt.Println("Guacamole SSH连接路由:")
	fmt.Println("  ws://localhost:8080/api/ssh/websocket")

	// 兼容路由
	fmt.Println("兼容SSH连接路由:")
	fmt.Println("  ws://localhost:8080/ws/ssh")

	// 终端调整示例
	fmt.Println("\n🔧 终端调整API示例:")
	terminalResizeExample()
}

// 终端调整示例
func terminalResizeExample() {
	// 模拟终端大小调整API调用
	params := url.Values{}
	params.Set("session_id", "ssh-session-001")
	params.Set("cols", "120")
	params.Set("rows", "40")

	fmt.Printf("终端调整请求: %s\n", params.Encode())

	// 模拟响应
	resizeResponse := map[string]interface{}{
		"success":    true,
		"message":    "Terminal resized successfully",
		"session_id": "ssh-session-001",
		"cols":       120,
		"rows":       40,
	}

	responseJSON, _ := json.MarshalIndent(resizeResponse, "", "  ")
	fmt.Printf("调整响应: %s\n", responseJSON)
}

// 打印响应信息
func printResponse(response SSHTunnelResponse) {
	fmt.Println("📋 连接响应:")
	responseJSON, _ := json.MarshalIndent(response, "", "  ")
	fmt.Println(string(responseJSON))

	// 展示兼容性信息
	fmt.Printf("✅ 连接状态: %v\n", response.Success)
	fmt.Printf("🔗 会话ID: %s\n", response.SessionID)
	fmt.Printf("🎯 目标: %s\n", response.Target)
	fmt.Printf("🛠️ 连接方式: %s\n", response.Method)

	if len(response.JumpHosts) > 0 {
		fmt.Printf("🔀 跳板机数量: %d\n", len(response.JumpHosts))
	}

	fmt.Println("---")
}

// 数据结构定义（与统一处理器保持一致）

type SSHTunnelRequest struct {
	Target          string            `json:"target"`
	Port            int               `json:"port"`
	Username        string            `json:"username"`
	Password        string            `json:"password"`
	PrivateKey      string            `json:"private_key,omitempty"`
	AuthType        string            `json:"auth_type,omitempty"`
	Cols            int               `json:"cols"`
	Rows            int               `json:"rows"`
	SessionID       string            `json:"session_id"`
	Method          string            `json:"method"` // "guacd", "direct", "auto"
	JumpHosts       []SSHJumpHost     `json:"jump_hosts"`
	EnableRecording bool              `json:"enable_recording"`
	Options         map[string]string `json:"options"`
}

type SSHJumpHost struct {
	Hostname   string `json:"hostname"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key,omitempty"`
	AuthMethod string `json:"auth_method"` // password, private_key
}

type SSHTunnelResponse struct {
	Success      bool                     `json:"success"`
	Message      string                   `json:"message"`
	SessionID    string                   `json:"session_id,omitempty"`
	ConnectionID string                   `json:"connection_id,omitempty"`
	Target       string                   `json:"target,omitempty"`
	JumpHosts    []map[string]interface{} `json:"jump_hosts,omitempty"`
	Timestamp    time.Time                `json:"timestamp"`
	Method       string                   `json:"method"` // "guacd" 或 "direct"
}
