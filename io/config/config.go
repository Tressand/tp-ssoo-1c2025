package config

import (
	"log/slog"
	"ssoo-utils/configManager"
	"ssoo-utils/httputils"
)

type IOConfig struct {
	IpKernel   string     `json:"ip_kernel"`
	PortKernel int        `json:"port_kernel"`
	LogLevel   slog.Level `json:"log_level"`
	PortIO     int        `json:"port_io"`
}

var Values IOConfig
var configFilePath string = "/config/io_config.json"

func SetFilePath(path string) {
	configFilePath = path
}

func Load() {
	configFilePath = configManager.GetDefaultExePath() + configFilePath

	err := configManager.LoadConfig(configFilePath, &Values)
	if err != nil {
		panic(err)
	}

	if Values.IpKernel == "self" {
		Values.IpKernel = httputils.GetOutboundIP()
	}
}
