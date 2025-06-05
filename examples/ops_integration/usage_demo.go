package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"newbee-agent/internal/client"
	"newbee-agent/internal/config"
	"newbee-agent/internal/handlers"
	"newbee-agent/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
)

// 示例：如何使用OPS流式客户端和任务处理器
func main() {
	// 1. 加载配置
	var c config.Config
	if err := conf.Load("../../etc/agent.yaml", &c); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. 设置日志
	if err := logx.SetUp(c.Log); err != nil {
		log.Fatalf("Failed to setup log: %v", err)
	}
	defer logx.Close()

	// 3. 创建服务上下文
	svcCtx := svc.NewServiceContext(c)

	// 4. 启动服务上下文（这会启动现有的任务执行器等组件）
	if err := svcCtx.Start(); err != nil {
		log.Fatalf("Failed to start service context: %v", err)
	}
	defer svcCtx.Stop()

	// 5. 创建OPS任务处理器
	opsTaskHandler := handlers.NewOpsTaskHandler(svcCtx)
	log.Println("OPS任务处理器已创建")

	// 6. 创建OPS流式客户端
	opsStreamClient := client.NewOpsStreamClient(&c, opsTaskHandler)
	log.Println("OPS流式客户端已创建")

	// 7. 启动OPS流式客户端
	if c.OpsRpc.Enabled {
		if err := opsStreamClient.Start(); err != nil {
			log.Fatalf("Failed to start OPS stream client: %v", err)
		}
		defer opsStreamClient.Stop()

		log.Println("OPS流式客户端已启动，开始监听任务...")

		// 8. 检查连接状态
		time.Sleep(2 * time.Second) // 等待连接建立
		if opsStreamClient.IsConnected() {
			log.Println("✅ 成功连接到OPS服务")
		} else {
			log.Println("❌ 无法连接到OPS服务")
		}
	} else {
		log.Println("OPS RPC已禁用，仅启动本地任务处理")
	}

	// 9. 演示任务处理器功能
	demonstrateTaskHandling(opsTaskHandler)

	// 10. 等待信号退出
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Agent正在运行，按Ctrl+C退出...")
	<-sigChan

	log.Println("正在优雅关闭...")
}

// demonstrateTaskHandling 演示任务处理器功能
func demonstrateTaskHandling(taskHandler *handlers.OpsTaskHandler) {
	log.Println("\n=== 演示任务处理器功能 ===")

	// 演示远程执行任务
	remoteTask := map[string]interface{}{
		"task_id":   "demo_remote_001",
		"task_type": "remote_execution",
		"target": map[string]interface{}{
			"host":     "192.168.1.100",
			"port":     22,
			"protocol": "ssh",
			"credentials": map[string]interface{}{
				"username": "demo",
				"password": "demo123",
			},
		},
		"command": map[string]interface{}{
			"content": "echo 'Hello from OPS task'",
			"timeout": 30,
		},
		"options": map[string]interface{}{
			"capture_output": true,
		},
	}

	log.Println("演示远程执行任务处理...")
	if err := taskHandler.HandleTask(remoteTask); err != nil {
		log.Printf("远程执行任务处理失败: %v", err)
	} else {
		log.Println("✅ 远程执行任务已提交")
	}

	// 演示文件传输任务
	transferTask := map[string]interface{}{
		"task_id":   "demo_transfer_001",
		"task_type": "file_transfer",
		"target": map[string]interface{}{
			"host":     "192.168.1.100",
			"port":     22,
			"protocol": "ssh",
			"credentials": map[string]interface{}{
				"username": "demo",
				"password": "demo123",
			},
		},
		"command": map[string]interface{}{
			"src_path":  "/tmp/local_file.txt",
			"dst_path":  "/tmp/remote_file.txt",
			"direction": "upload",
			"timeout":   60,
		},
	}

	log.Println("演示文件传输任务处理...")
	if err := taskHandler.HandleTask(transferTask); err != nil {
		log.Printf("文件传输任务处理失败: %v", err)
	} else {
		log.Println("✅ 文件传输任务已提交")
	}

	// 演示健康检查任务
	healthTask := map[string]interface{}{
		"task_id":   "demo_health_001",
		"task_type": "health_check",
	}

	log.Println("演示健康检查任务处理...")
	if err := taskHandler.HandleTask(healthTask); err != nil {
		log.Printf("健康检查任务处理失败: %v", err)
	} else {
		log.Println("✅ 健康检查任务已完成")
	}

	// 演示配置更新任务
	configTask := map[string]interface{}{
		"task_id":   "demo_config_001",
		"task_type": "config_update",
		"config_updates": map[string]interface{}{
			"log_level":       "debug",
			"max_connections": 100,
		},
	}

	log.Println("演示配置更新任务处理...")
	if err := taskHandler.HandleTask(configTask); err != nil {
		log.Printf("配置更新任务处理失败: %v", err)
	} else {
		log.Println("✅ 配置更新任务已完成")
	}

	log.Println("=== 任务处理器功能演示完成 ===\n")
}

/*
使用说明：

1. 配置文件设置：
   确保 etc/agent.yaml 中的 OpsRpc 配置正确：

   OpsRpc:
     Enabled: true
     Endpoints:
       - "localhost:8081"
     Timeout: 5000

2. 运行方式：
   cd agent/examples/ops_integration
   go run usage_demo.go

3. 功能说明：
   - 创建和启动OPS流式客户端
   - 实现TaskHandler接口处理OPS任务
   - 支持远程执行、文件传输、健康检查、配置更新等任务
   - 自动连接注册、心跳维持、双向通信

4. 任务类型：
   - remote_execution: SSH/Telnet远程命令执行
   - file_transfer: 文件上传/下载
   - health_check: Agent健康状态检查
   - config_update: 动态配置更新

5. 扩展方式：
   - 在 handlers.OpsTaskHandler.HandleTask 中添加新的任务类型
   - 在 client.OpsStreamClient 中扩展消息处理逻辑
   - 根据需要实现自定义的TaskHandler
*/
