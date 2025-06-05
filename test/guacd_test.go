package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const baseURL = "http://localhost:8889"

// HealthResponse 健康检查响应结构
type HealthResponse struct {
	Status            string                 `json:"status"`
	AgentID           string                 `json:"agent_id"`
	Version           string                 `json:"version"`
	GuacdStatus       string                 `json:"guacd_status"`
	PluginDetails     map[string]interface{} `json:"plugin_details"`
	SupportedFeatures map[string]bool        `json:"supported_features"`
}

func main() {
	fmt.Println("=== NewBee Agent guacd功能测试 ===")

	// 测试1: 健康检查
	fmt.Println("\n1. 测试健康检查接口...")
	if err := testHealthCheck(); err != nil {
		fmt.Printf("❌ 健康检查失败: %v\n", err)
		return
	}

	// 测试2: 插件状态
	fmt.Println("\n2. 测试插件状态接口...")
	if err := testPlugins(); err != nil {
		fmt.Printf("❌ 插件状态测试失败: %v\n", err)
	}

	// 测试3: 指标接口
	fmt.Println("\n3. 测试指标接口...")
	if err := testMetrics(); err != nil {
		fmt.Printf("❌ 指标测试失败: %v\n", err)
	}

	// 测试4: guacd路由可访问性
	fmt.Println("\n4. 测试guacd相关路由可访问性...")
	testGuacdRoutes()

	// 测试5: WebSocket连接测试
	fmt.Println("\n5. 测试WebSocket连接...")
	if err := testWebSocketConnection(); err != nil {
		fmt.Printf("❌ WebSocket连接测试失败: %v\n", err)
	}

	fmt.Println("\n=== 测试完成 ===")
}

func testHealthCheck() error {
	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %v", err)
	}

	var health HealthResponse
	if err := json.Unmarshal(body, &health); err != nil {
		return fmt.Errorf("解析JSON失败: %v", err)
	}

	fmt.Printf("✅ 健康检查成功\n")
	fmt.Printf("   Agent状态: %s\n", health.Status)
	fmt.Printf("   Agent ID: %s\n", health.AgentID)
	fmt.Printf("   版本: %s\n", health.Version)
	fmt.Printf("   Guacd状态: %s\n", health.GuacdStatus)

	if health.GuacdStatus == "disconnected" {
		fmt.Println("⚠️  Guacd服务连接失败")
		fmt.Println("   可能的原因:")
		fmt.Println("   1. Guacd服务未启动")
		fmt.Println("   2. 网络连接问题 (192.168.26.130:4822)")
		fmt.Println("   3. 防火墙阻止连接")
		fmt.Println("   4. 配置地址错误")
	} else {
		fmt.Println("✅ Guacd服务连接正常")
	}

	// 显示插件详情
	if len(health.PluginDetails) > 0 {
		fmt.Println("\n   插件状态:")
		for name, details := range health.PluginDetails {
			fmt.Printf("   - %s: %+v\n", name, details)
		}
	}

	// 显示支持的功能
	if len(health.SupportedFeatures) > 0 {
		fmt.Println("\n   支持的功能:")
		for feature, enabled := range health.SupportedFeatures {
			status := "❌"
			if enabled {
				status = "✅"
			}
			fmt.Printf("   %s %s\n", status, feature)
		}
	}

	return nil
}

func testPlugins() error {
	resp, err := http.Get(baseURL + "/plugins")
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %v", err)
	}

	var plugins map[string]interface{}
	if err := json.Unmarshal(body, &plugins); err != nil {
		return fmt.Errorf("解析JSON失败: %v", err)
	}

	fmt.Printf("✅ 插件状态获取成功\n")
	fmt.Printf("   已加载插件数量: %v\n", plugins["total_count"])
	if loadedPlugins, ok := plugins["loaded_plugins"].([]interface{}); ok {
		fmt.Printf("   插件列表: %v\n", loadedPlugins)
	}

	return nil
}

func testMetrics() error {
	resp, err := http.Get(baseURL + "/metrics")
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %v", err)
	}

	var metrics map[string]interface{}
	if err := json.Unmarshal(body, &metrics); err != nil {
		return fmt.Errorf("解析JSON失败: %v", err)
	}

	fmt.Printf("✅ 指标获取成功\n")
	fmt.Printf("   活跃会话数: %v\n", metrics["active_sessions"])
	fmt.Printf("   已加载插件数: %v\n", metrics["loaded_plugins"])

	return nil
}

func testGuacdRoutes() {
	routes := []string{
		"/api/rdp/websocket",
		"/api/ssh/websocket",
		"/api/vnc/websocket",
		"/api/telnet/websocket",
		"/api/rdp/guacamole",
		"/guacamole",
	}

	for _, route := range routes {
		resp, err := http.Get(baseURL + route)
		if err != nil {
			fmt.Printf("❌ %s - 请求失败: %v\n", route, err)
			continue
		}
		resp.Body.Close()

		switch resp.StatusCode {
		case 200:
			fmt.Printf("✅ %s - 路由可访问\n", route)
		case 400:
			fmt.Printf("✅ %s - WebSocket路由正常（期望400错误）\n", route)
		case 404:
			fmt.Printf("❌ %s - 路由不存在（404）\n", route)
		default:
			fmt.Printf("⚠️  %s - 状态码: %d\n", route, resp.StatusCode)
		}
	}
}

func testWebSocketConnection() error {
	// 测试WebSocket连接
	wsURL := "ws://localhost:8889/api/rdp/websocket?hostname=test&username=test&password=test"

	fmt.Printf("   尝试连接: %s\n", wsURL)

	// 设置WebSocket连接超时
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil {
			fmt.Printf("   HTTP状态码: %d\n", resp.StatusCode)
			if resp.StatusCode == 400 {
				fmt.Printf("✅ WebSocket路由存在（收到400错误是正常的，因为缺少有效的guacd连接）\n")
				return nil
			}
		}
		return fmt.Errorf("WebSocket连接失败: %v", err)
	}
	defer conn.Close()

	fmt.Printf("✅ WebSocket连接建立成功\n")

	// 发送测试消息
	testMessage := "test message"
	if err := conn.WriteMessage(websocket.TextMessage, []byte(testMessage)); err != nil {
		return fmt.Errorf("发送消息失败: %v", err)
	}
	fmt.Printf("   已发送测试消息: %s\n", testMessage)

	// 设置读取超时
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// 尝试读取响应
	_, message, err := conn.ReadMessage()
	if err != nil {
		// 如果是超时错误，这是正常的（因为可能没有真实的guacd后端）
		if websocket.IsCloseError(err, websocket.CloseAbnormalClosure) ||
			websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
			fmt.Printf("   连接正常关闭（这是预期的，因为没有真实的RDP后端）\n")
			return nil
		}
		fmt.Printf("   读取超时或连接关闭（这是正常的）: %v\n", err)
		return nil
	}

	fmt.Printf("   收到响应: %s\n", string(message))

	return nil
}
