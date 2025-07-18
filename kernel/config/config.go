package config

import (
	"log/slog"
	"path/filepath"
	"ssoo-utils/configManager"
	"ssoo-utils/httputils"
)

type KernelConfig struct {
	IpMemory              string     `json:"ip_memory"`
	PortMemory            int        `json:"port_memory"`
	PortKernel            int        `json:"port_kernel"`
	SchedulerAlgorithm    string     `json:"scheduler_algorithm"`
	ReadyIngressAlgorithm string     `json:"ready_ingress_algorithm"`
	Alpha                 float64    `json:"alpha"`
	SuspensionTime        int        `json:"suspension_time"`
	LogLevel              slog.Level `json:"log_level"`
	CodeFolder            string     `json:"code_folder"`
	InitialEstimate       int64    `json:"initial_estimate"`
}

var Values KernelConfig
var configFilePath string = "/config/kernel_config.json"

func SetFilePath(path string) {
	configFilePath = path
}

func Load() {
	configFilePath = configManager.GetDefaultExePath() + configFilePath

	err := configManager.LoadConfig(configFilePath, &Values)
	if err != nil {
		panic(err)
	}

	if Values.IpMemory == "self" {
		Values.IpMemory = httputils.GetOutboundIP()
	}

	Values.CodeFolder, _ = filepath.Abs(configManager.GetDefaultExePath() + "/../code")
}
