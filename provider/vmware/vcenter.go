package vmware

import (
	"context"
	"fmt"

	"github.com/coder-lulu/newbee-io/provider"
)

type VCenterProvider struct {
}

func NewVCenterProvider() *VCenterProvider {
	return &VCenterProvider{}
}

func (p *VCenterProvider) GetMetadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		ID:          "vmware_vcenter",
		Name:        "VMware vCenter",
		Category:    "api",
		Description: "通过vCenter API发现虚拟机资源",
		Version:     "1.0.0",
		Icon:        "/icons/vmware.svg",
	}
}

func (p *VCenterProvider) GetParameterSchema() []provider.ParameterDefinition {
	return []provider.ParameterDefinition{
		{
			Name:        "host",
			Label:       "vCenter地址",
			Type:        "string",
			Required:    true,
			Placeholder: "vcenter.example.com",
			Validation:  &provider.ValidationRule{Pattern: "hostname"},
		},
		{
			Name:     "username",
			Label:    "用户名",
			Type:     "string",
			Required: true,
		},
		{
			Name:     "password",
			Label:    "密码",
			Type:     "password",
			Required: true,
		},
		{
			Name:         "port",
			Label:        "端口",
			Type:         "int",
			Required:     false,
			DefaultValue: 443,
		},
		{
			Name:         "verify_ssl",
			Label:        "验证SSL证书",
			Type:         "boolean",
			Required:     false,
			DefaultValue: true,
		},
	}
}

func (p *VCenterProvider) GetFieldSchema() []provider.FieldDefinition {
	return []provider.FieldDefinition{
		{Name: "name", Label: "虚拟机名称", DataType: "string", Required: true},
		{Name: "ipAddress", Label: "IP地址", DataType: "string"},
		{Name: "numCPU", Label: "CPU数量", DataType: "integer"},
		{Name: "memorySizeMB", Label: "内存大小(MB)", DataType: "integer"},
		{Name: "diskSizeGB", Label: "磁盘大小(GB)", DataType: "float"},
		{Name: "powerState", Label: "电源状态", DataType: "string"},
		{Name: "guestOS", Label: "客户机操作系统", DataType: "string"},
		{Name: "vmwareToolsStatus", Label: "VMware Tools状态", DataType: "string"},
		{Name: "cluster", Label: "集群名称", DataType: "string"},
		{Name: "datacenter", Label: "数据中心", DataType: "string"},
	}
}

func (p *VCenterProvider) Initialize(config map[string]interface{}) error {
	return nil
}

func (p *VCenterProvider) TestConnection(config map[string]interface{}) error {
	return fmt.Errorf("not implemented yet")
}

func (p *VCenterProvider) Discover(ctx context.Context, config map[string]interface{}) ([]map[string]interface{}, error) {
	return nil, fmt.Errorf("not implemented yet")
}

func (p *VCenterProvider) ValidateMapping(mapping []provider.FieldMapping) error {
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
