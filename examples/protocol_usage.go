package main

import (
	"fmt"
	"net/url"
	"time"

	"newbee-agent/common/guacd"
)

func main() {
	fmt.Println("🚀 Guacd 通用架构演示")
	fmt.Println("========================")

	// 创建管理器
	config := &guacd.Config{
		Address:        "192.168.26.130:4822",
		Fallbacks:      []string{"localhost:4822"},
		ConnectTimeout: 10 * time.Second,
	}
	manager := guacd.NewManager(config)

	// 演示RDP连接
	fmt.Println("\n📡 RDP连接示例:")
	fmt.Println("===============")
	rdpExample(manager)

	// 演示SSH连接
	fmt.Println("\n🔧 SSH连接示例:")
	fmt.Println("===============")
	sshExample(manager)

	// 演示VNC连接
	fmt.Println("\n🖥️ VNC连接示例:")
	fmt.Println("===============")
	vncExample(manager)

	// 演示TELNET连接
	fmt.Println("\n📞 TELNET连接示例:")
	fmt.Println("=================")
	telnetExample(manager)

	fmt.Println("\n✅ 所有示例创建完成！")
}

func rdpExample(manager *guacd.Manager) {
	// 方式1: 使用便捷方法
	fmt.Println("方式1 - 便捷连接:")
	tunnel, err := manager.ConnectRDP("192.168.26.140", "work", "931214", 1920, 1080)
	if err != nil {
		fmt.Printf("  ❌ RDP连接失败: %v\n", err)
	} else {
		fmt.Printf("  ✅ RDP连接成功，ID: %s\n", tunnel.GetUUID())
		tunnel.Close()
	}

	// 方式2: 使用URL参数 (高级配置)
	fmt.Println("方式2 - 高级配置:")
	query := url.Values{}
	query.Set("scheme", "rdp")
	query.Set("hostname", "192.168.26.140")
	query.Set("username", "work")
	query.Set("password", "931214")
	query.Set("width", "1920")
	query.Set("height", "1080")
	query.Set("security", "any")
	query.Set("ignore_cert", "true")
	query.Set("enable_drive", "false")

	tunnel2, err := manager.ConnectToProtocol(query, nil, "work")
	if err != nil {
		fmt.Printf("  ❌ RDP高级连接失败: %v\n", err)
	} else {
		fmt.Printf("  ✅ RDP高级连接成功，ID: %s\n", tunnel2.GetUUID())
		tunnel2.Close()
	}
}

func sshExample(manager *guacd.Manager) {
	// SSH连接示例
	fmt.Println("SSH连接配置:")
	query := url.Values{}
	query.Set("scheme", "ssh")
	query.Set("hostname", "192.168.1.100")
	query.Set("username", "root")
	query.Set("password", "password")
	query.Set("font_name", "monospace")
	query.Set("font_size", "14")
	query.Set("color_scheme", "green-black")
	query.Set("terminal_type", "xterm-256color")
	query.Set("enable_sftp", "true")

	tunnel, err := manager.ConnectToProtocol(query, nil, "root")
	if err != nil {
		fmt.Printf("  ❌ SSH连接失败: %v\n", err)
	} else {
		fmt.Printf("  ✅ SSH连接成功，ID: %s\n", tunnel.GetUUID())
		tunnel.Close()
	}
}

func vncExample(manager *guacd.Manager) {
	// VNC连接示例
	fmt.Println("VNC连接配置:")
	query := url.Values{}
	query.Set("scheme", "vnc")
	query.Set("hostname", "192.168.1.101")
	query.Set("password", "vncpass")
	query.Set("width", "1024")
	query.Set("height", "768")
	query.Set("read_only", "false")
	query.Set("cursor", "local")
	query.Set("color_depth", "24")

	tunnel, err := manager.ConnectToProtocol(query, nil, "vnc-user")
	if err != nil {
		fmt.Printf("  ❌ VNC连接失败: %v\n", err)
	} else {
		fmt.Printf("  ✅ VNC连接成功，ID: %s\n", tunnel.GetUUID())
		tunnel.Close()
	}
}

func telnetExample(manager *guacd.Manager) {
	// TELNET连接示例
	fmt.Println("TELNET连接配置:")
	query := url.Values{}
	query.Set("scheme", "telnet")
	query.Set("hostname", "192.168.1.102")
	query.Set("username", "admin")
	query.Set("password", "admin")
	query.Set("terminal_type", "ansi")
	query.Set("username_regex", "[Ll]ogin:")
	query.Set("password_regex", "[Pp]assword:")

	tunnel, err := manager.ConnectToProtocol(query, nil, "admin")
	if err != nil {
		fmt.Printf("  ❌ TELNET连接失败: %v\n", err)
	} else {
		fmt.Printf("  ✅ TELNET连接成功，ID: %s\n", tunnel.GetUUID())
		tunnel.Close()
	}
}
