package config

import (
	"fmt"
	"log/slog"
	"ssoo-utils/configManager"
	"ssoo-utils/httputils"
)

type KernelConfig struct {
	MemoryURL          string
	IpMemory           string     `json:"ip_memory"`
	PortMemory         int        `json:"port_memory"`
	PortKernel         int        `json:"port_kernel"`
	SchedulerAlgorithm string     `json:"scheduler_algorithm"`
	SuspensionTime     int        `json:"suspension_time"`
	LogLevel           slog.Level `json:"log_level"`
}

var Values KernelConfig
var configFilePath string = "/config/kernel_config.json"

func SetFilePath(path string) {
	configFilePath = path
}

func Load() {
	configFilePath = configManager.GetDefaultConfigPath() + configFilePath

	err := configManager.LoadConfig(configFilePath, &Values)
	if err != nil {
		panic(err)
	}

	if Values.IpMemory == "self" {
		Values.IpMemory = httputils.GetOutboundIP()
	}
	Values.MemoryURL = "http://" + Values.IpMemory + ":" + fmt.Sprint(Values.PortMemory)
}
