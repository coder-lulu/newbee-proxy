package provider

import (
    "github.com/coder-lulu/newbee-io/provider"
    "github.com/coder-lulu/newbee-proxy/provider/nb_agent"
    "github.com/coder-lulu/newbee-proxy/provider/vmware"
    "github.com/zeromicro/go-zero/core/logx"
)

// InitProviders 初始化所有Provider并注册到全局Registry
func InitProviders() {
	// 注册nb_agent Provider（核心Provider）
	if err := provider.Register(nb_agent.NewNbAgentProvider()); err != nil {
		logx.Errorw("Failed to register nb_agent provider", logx.Field("error", err.Error()))
	} else {
		logx.Info("Successfully registered nb_agent provider")
	}

	// 注册VMware vCenter Provider
	if err := provider.Register(vmware.NewVCenterProvider()); err != nil {
		logx.Errorw("Failed to register vmware_vcenter provider", logx.Field("error", err.Error()))
	} else {
		logx.Info("Successfully registered vmware_vcenter provider")
	}

	// TODO: 注册其他Provider
	// provider.Register(linux.NewLinuxSshProvider())
	// provider.Register(windows.NewWindowsWmiProvider())
	// provider.Register(aliyun.NewAliyunEcsProvider())
	// provider.Register(tencent.NewTencentCvmProvider())
	// provider.Register(aws.NewAwsEc2Provider())
	// provider.Register(huawei.NewHuaweiEcsProvider())
	// provider.Register(azure.NewAzureVmProvider())
	// provider.Register(openstack.NewOpenstackProvider())
	// provider.Register(snmp.NewSnmpDeviceProvider())
	// provider.Register(ssh_network.NewSshNetworkProvider())
	// provider.Register(file.NewFileImportProvider())
	// provider.Register(api.NewApiCustomProvider())

	logx.Infof("Provider initialization completed, total registered: %d", len(provider.ListProviders()))
}
