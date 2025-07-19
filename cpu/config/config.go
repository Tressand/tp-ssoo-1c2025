package config

import (
	"fmt"
	"log/slog"
	"ssoo-utils/codeutils"
	"ssoo-utils/configManager"
	"ssoo-utils/httputils"
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

type PaginationConfig struct {
	PageSize       int `json:"page_size"`
	EntriesPerPage int `json:"entries_per_page"`
	Levels         int `json:"levels"`
}

type PCBS struct {
	PID int
	PC  int
	ME  []int
	MT  []int
}

type Exec_valuesS struct {
	Arg1  int
	Arg2  int
	Str   string
	Addr  []int
	Value []byte
}

type RequestPayload struct {
	PID int `json:"pid"`
	PC  int `json:"pc"`
}

type KernelResponse struct {
	PID int `json:"pid"`
	PC  int `json:"pc"`
}

type DispatchResponse struct {
	PID    int    `json:"pid"`
	PC     int    `json:"pc"`
	Motivo string `json:"motivo"`
}

type Tlb_entries struct {
	Page     []int
	Frame    int
	LastUsed int64
	Pid int
}

type TLB struct {
	Entries        []Tlb_entries
	Capacity       int
	ReplacementAlg string
}

type CACHE struct {
	Entries        []CacheEntry
	Capacity       int
	ReplacementAlg string
	Delay          int
}

type CacheEntry struct {
	Page     []int
	Content  []byte
	Use      bool
	Modified bool
	Position bool //para saber si me quede aca o en otra posicion
	Pid      int
}

type ResponsePayload = codeutils.Instruction

var Values CPUConfig
var MemoryConf PaginationConfig
var Pcb PCBS
var Exec_values = Exec_valuesS{
	Arg1:  -1,
	Arg2:  -1,
	Str:   "",
	Addr:  []int{0},
	Value: []byte{0},
}
var Instruccion string
var Identificador int
var configFilePath string = "/config"
var (
	InterruptChan = make(chan struct{}, 1) // Buffer para 1 señal
	ExitChan      = make(chan struct{}, 1)
	FinishBeforeInterrupt = make(chan struct{}, 1)
)

var KernelResp KernelResponse

var Tlb TLB
var Cache CACHE

var CacheEnable bool = false

func SetFilePath(path string) {
	configFilePath = path
}

func Load() {
	configFilePath = configManager.GetDefaultExePath() + configFilePath + "/cpu" + fmt.Sprint(Identificador) + "_config.json"

	err := configManager.LoadConfig(configFilePath, &Values)
	if err != nil {
		panic(err)
	}

	if Values.IpMemory == "self" {
		Values.IpMemory = httputils.GetOutboundIP()
	}
	if Values.IpKernel == "self" {
		Values.IpKernel = httputils.GetOutboundIP()
	}
}
