package config

import (
	"fmt"
	"log/slog"
	"path/filepath"
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
var configFilePath string = "./io/config/config.json"

func SetFilePath(path string) {
	configFilePath = path
}

func Load() {
	filepath, err := filepath.Abs(configFilePath)
	if err != nil {
		panic(err)
	}

	err = configManager.LoadConfig(filepath, &Values)
	if err != nil {
		panic(err)
	}

	if Values.IpKernel == "self" {
		Values.IpKernel = httputils.GetOutboundIP()
	}
	Values.KernelURL = "http://" + Values.IpKernel + ":" + fmt.Sprint(Values.PortKernel)
}
