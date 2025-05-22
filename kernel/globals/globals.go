package globals

import (
	config "ssoo-kernel/config"
	"ssoo-utils/pcb"
	"sync"
)

var (
	SchedulerStatus  string
	NextPID          uint = 0
	PIDMutex         sync.Mutex
	LTS              []Process = make([]Process, 0)
	LTSMutex         sync.Mutex
	STS              []Process = make([]Process, 0)
	STSMutex         sync.Mutex
	ReadySusp        []Process = make([]Process, 0) // Temporal
	LTSEmpty                   = make(chan struct{})
	STSEmpty                   = make(chan struct{})
	AvailableCpu               = make(chan struct{}, 1)
	PCBReceived                = make(chan struct{}, 1)
	AvailableCPUs    []CPUConnection
	CpuListMutex     sync.Mutex
	RetryProcessCh   = make(chan struct{}) // Esto deberia ser activado luego en Finalización de procesos
	WaitingForMemory = make(chan struct{}, 1)
	WaitingForCPU    = make(chan struct{}, 1)
)

type CPUConnection struct {
	ID      string
	IP      string
	Port    int
	Working bool
}

type DispatchResponse struct {
	PID    int    `json:"pid"`
	PC     int    `json:"pc"`
	Motivo string `json:"motivo"`
}

type CPURequest struct {
	PID uint `json:"pid"`
	PC  int  `json:"pc"`
}

type Process struct {
	PCB  *pcb.PCB
	Path string
	Size int
}

func (p Process) GetPath() string { return config.Values.CodeFolder + "/" + p.Path }

func (p Process) GetSize() int { return p.Size }
