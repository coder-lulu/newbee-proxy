package nb_agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/coder-lulu/newbee-io/provider"
)

type NbAgentProvider struct {
	client *http.Client
}

func NewNbAgentProvider() *NbAgentProvider {
	return &NbAgentProvider{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *NbAgentProvider) GetMetadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		ID:          "nb_agent",
		Name:        "NewBee Agent",
		Category:    "agent",
		Description: "通过NewBee Agent自动发现和采集服务器信息",
		Version:     "1.0.0",
		Icon:        "/icons/nb-agent.svg",
	}
}

func (p *NbAgentProvider) GetParameterSchema() []provider.ParameterDefinition {
	return []provider.ParameterDefinition{
		{
			Name:        "agent_endpoint",
			Label:       "Agent地址",
			Type:        "string",
			Required:    true,
			Placeholder: "http://192.168.1.100:8888",
			Description: "nb-agent的HTTP API地址",
		},
		{
			Name:        "auth_token",
			Label:       "认证Token",
			Type:        "password",
			Required:    true,
			Description: "Agent API认证令牌",
		},
		{
			Name:         "timeout_seconds",
			Label:        "超时时间(秒)",
			Type:         "int",
			Required:     false,
			DefaultValue: 30,
		},
	}
}

func (p *NbAgentProvider) GetFieldSchema() []provider.FieldDefinition {
	return []provider.FieldDefinition{
		{
			Name:        "hostname",
			Label:       "主机名",
			DataType:    "string",
			Required:    true,
			Description: "服务器主机名",
		},
		{
			Name:     "ip_address",
			Label:    "IP地址",
			DataType: "string",
			Required: true,
			Example:  "192.168.1.100",
		},
		{
			Name:     "os_type",
			Label:    "操作系统类型",
			DataType: "string",
			Required: true,
			Example:  "Linux",
		},
		{
			Name:     "os_version",
			Label:    "操作系统版本",
			DataType: "string",
			Example:  "CentOS 7.9",
		},
		{
			Name:     "kernel_version",
			Label:    "内核版本",
			DataType: "string",
			Example:  "3.10.0-1160.el7.x86_64",
		},
		{
			Name:     "cpu_model",
			Label:    "CPU型号",
			DataType: "string",
		},
		{
			Name:     "cpu_cores",
			Label:    "CPU核心数",
			DataType: "integer",
			Example:  "8",
		},
		{
			Name:     "memory_total_gb",
			Label:    "内存总量(GB)",
			DataType: "float",
			Example:  "16.0",
		},
		{
			Name:     "disk_total_gb",
			Label:    "磁盘总量(GB)",
			DataType: "float",
		},
		{
			Name:     "agent_version",
			Label:    "Agent版本",
			DataType: "string",
		},
		{
			Name:     "uptime_seconds",
			Label:    "运行时间(秒)",
			DataType: "integer",
		},
	}
}

func (p *NbAgentProvider) Initialize(config map[string]interface{}) error {
	return nil
}

func (p *NbAgentProvider) TestConnection(config map[string]interface{}) error {
	endpoint, ok := config["agent_endpoint"].(string)
	if !ok {
		return fmt.Errorf("agent_endpoint is required")
	}

	token, ok := config["auth_token"].(string)
	if !ok {
		return fmt.Errorf("auth_token is required")
	}

	req, err := http.NewRequest("GET", endpoint+"/api/v1/system/info", nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (p *NbAgentProvider) Discover(ctx context.Context, config map[string]interface{}) ([]map[string]interface{}, error) {
	endpoint, ok := config["agent_endpoint"].(string)
	if !ok {
		return nil, fmt.Errorf("agent_endpoint is required")
	}

	token, ok := config["auth_token"].(string)
	if !ok {
		return nil, fmt.Errorf("auth_token is required")
	}

	req, err := http.NewRequest("GET", endpoint+"/api/v1/system/info", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req = req.WithContext(ctx)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to discover from agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent returned status %d: %s", resp.StatusCode, string(body))
	}

	var sysInfo struct {
		Data struct {
			Hostname      string  `json:"hostname"`
			IP            string  `json:"ip"`
			OS            string  `json:"os"`
			OSVersion     string  `json:"osVersion"`
			KernelVersion string  `json:"kernelVersion"`
			CPUModel      string  `json:"cpuModel"`
			CPUCores      int     `json:"cpuCores"`
			MemoryTotal   float64 `json:"memoryTotal"`
			DiskTotal     float64 `json:"diskTotal"`
			AgentVersion  string  `json:"agentVersion"`
			Uptime        int     `json:"uptime"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&sysInfo); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	result := map[string]interface{}{
		"hostname":         sysInfo.Data.Hostname,
		"ip_address":       sysInfo.Data.IP,
		"os_type":          sysInfo.Data.OS,
		"os_version":       sysInfo.Data.OSVersion,
		"kernel_version":   sysInfo.Data.KernelVersion,
		"cpu_model":        sysInfo.Data.CPUModel,
		"cpu_cores":        sysInfo.Data.CPUCores,
		"memory_total_gb":  sysInfo.Data.MemoryTotal / 1024 / 1024 / 1024,
		"disk_total_gb":    sysInfo.Data.DiskTotal / 1024 / 1024 / 1024,
		"agent_version":    sysInfo.Data.AgentVersion,
		"uptime_seconds":   sysInfo.Data.Uptime,
	}

	return []map[string]interface{}{result}, nil
}

func (p *NbAgentProvider) ValidateMapping(mapping []provider.FieldMapping) error {
	fields := make(map[string]bool)
	for _, f := range p.GetFieldSchema() {
		fields[f.Name] = true
	}

	for _, m := range mapping {
		if !fields[m.SourceField] {
			return fmt.Errorf("invalid source field: %s", m.SourceField)
		}
	}

	return nil
}
