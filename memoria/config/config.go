package config

import (
	"log/slog"
	"path/filepath"
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

var Config MemoryConfig

func Load() {
	filepath, err := filepath.Abs("./memoria/config/config.json")
	if err != nil {
		panic(err)
	}

	err = configManager.LoadConfig(filepath, &Config)
	if err != nil {
		panic(err)
	}
}
