package config

import (
	"log/slog"
	"path/filepath"
	"ssoo-utils/configManager"
)

type CPUConfig struct {
	PortCPU          int        `json:"port_cpu"`
	IpMemory         string     `json:"ip_memory"`
	PortMemory       int        `json:"port_memory"`
	IpKernel         string     `json:"ip_kernel"`
	PortKernel       int        `json:"port_kernel"`
	TLBEntries       int        `json:"tlb_entries"`
	TLBReplacement   string     `json:"tlb_replacement"`
	CacheEntries     int        `json:"cache_entries"`
	CacheReplacement string     `json:"cache_replacement"`
	CacheDelay       int        `json:"cache_delay"`
	LogLevel         slog.Level `json:"log_level"`
}

type PCB struct{
	PID int
	PC int
	ME []int
	MT []int
}

type Registros struct{
	AX,BX,CX,DX int
}

type Exec_values struct{
	arg1,arg2 int
}

var Values CPUConfig
var configFilePath string = "./cpu/config/config.json"
var pcb PCB
var registros Registros
var exec_values Exec_values

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
}
