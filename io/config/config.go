package config

import (
	"fmt"
	"log/slog"
	"ssoo-utils/configManager"
	"ssoo-utils/httputils"
)

type IOConfig struct {
	KernelURL  string
	IpKernel   string     `json:"ip_kernel"`
	PortKernel int        `json:"port_kernel"`
	PortIO     int        `json:"port_io"`
	LogLevel   slog.Level `json:"log_level"`
}

var Values IOConfig
var configFilePath string = "/config/io_config.json"

func SetFilePath(path string) {
	configFilePath = path
}

func Load() {
	configFilePath = configManager.GetDefaultConfigPath() + configFilePath

	err := configManager.LoadConfig(configFilePath, &Values)
	if err != nil {
		panic(err)
	}

	if Values.IpKernel == "self" {
		Values.IpKernel = httputils.GetOutboundIP()
	}
	Values.KernelURL = "http://" + Values.IpKernel + ":" + fmt.Sprint(Values.PortKernel)
}
