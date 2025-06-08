package config

import (
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

type PCBS struct {
	PID int
	PC  int
	ME  []int
	MT  []int
}

type Exec_valuesS struct {
	Arg1 int
	Arg2 int
	Str string
}

type RequestPayload struct {
	PID int `json:"pid"`
	PC  int `json:"pc"`
}

type KernelResponse struct{
	PID int `json:"pid"`
	PC int `json:"pc"`
}

type DispatchResponse struct {
	PID    int    `json:"pid"`
	PC     int    `json:"pc"`
	Motivo string `json:"motivo"`
}

type Tlb_entries struct{
	Page []int
	Frame int
	LastUsed int64
}

type TLB struct {
	Entries []Tlb_entries
	Capacity int
	ReplacementAlg string
}

type Logic_Direction struct{
	entrys []int
	scrolling int
}

type ResponsePayload = codeutils.Instruction

var Values CPUConfig
var Pcb PCBS
var Exec_values = Exec_valuesS{
	Arg1: -1,
	Arg2: -1,
	Str: "",
}
var Instruccion string
var Identificador int
var configFilePath string = "/config/cpu_config.json"

var InterruptChan  chan string = make(chan string)
var ExitChan  chan string = make(chan string)
var CicloDone chan string = make(chan string)

var KernelResp KernelResponse

var Tlb TLB

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
	if Values.IpKernel == "self" {
		Values.IpKernel = httputils.GetOutboundIP()
	}
}
