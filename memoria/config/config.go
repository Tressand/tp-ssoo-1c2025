package config

import (
	"log/slog"
	"ssoo-utils/configManager"
)

type MemoryConfig struct {
	PortMemory     int        `json:"port_memory"`
	MemorySize     int        `json:"memory_size"`
	PageSize       int        `json:"page_size"`
	EntriesPerPage int        `json:"entries_per_page"`
	NumberOfLevels int        `json:"number_of_levels"`
	MemoryDelay    int        `json:"memory_delay"`
	SwapfilePath   string     `json:"swapfile_path"`
	SwapDelay      int        `json:"swap_delay"`
	DumpPath       string     `json:"dump_path"`
	LogLevel       slog.Level `json:"log_level"`
}

var Values MemoryConfig
var configFilePath string = "/config/memoria_config.json"

func SetFilePath(path string) {
	configFilePath = path
}

func Load() {
	configFilePath = configManager.GetDefaultExePath() + configFilePath

	err := configManager.LoadConfig(configFilePath, &Values)
	if err != nil {
		panic(err)
	}
	Values.DumpPath = configManager.GetDefaultExePath() + Values.DumpPath
	Values.SwapfilePath = configManager.GetDefaultExePath() + Values.SwapfilePath
}
