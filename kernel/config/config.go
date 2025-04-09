package config

import (
	"log/slog"
	"path/filepath"
	"ssoo-utils/configManager"
)

type KernelConfig struct {
	IpMemory           string     `json:"ip_memory"`
	PortMemory         int        `json:"port_memory"`
	PortKernel         int        `json:"port_kernel"`
	SchedulerAlgorithm string     `json:"scheduler_algorithm"`
	SuspensionTime     int        `json:"suspension_time"`
	LogLevel           slog.Level `json:"log_level"`
}

var Config KernelConfig
var configFilePath string = "./kernel/config/config.json"

func SetFilePath(path string) {
	configFilePath = path
}

func Load() {
	filepath, err := filepath.Abs(configFilePath)
	if err != nil {
		panic(err)
	}

	err = configManager.LoadConfig(filepath, &Config)
	if err != nil {
		panic(err)
	}
}
