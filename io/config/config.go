package config

import (
	"log/slog"
	"path/filepath"
	"ssoo-utils/configManager"
)

type IOConfig struct {
	IpKernel   string     `json:"ip_kernel"`
	PortKernel int        `json:"port_kernel"`
	PortIO     int        `json:"port_io"`
	LogLevel   slog.Level `json:"log_level"`
}

var Config IOConfig

func Load() {
	filepath, err := filepath.Abs("./io/config/config.json")
	if err != nil {
		panic(err)
	}

	err = configManager.LoadConfig(filepath, &Config)
	if err != nil {
		panic(err)
	}
}
