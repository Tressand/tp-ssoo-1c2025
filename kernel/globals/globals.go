package globals

import (
	config "ssoo-kernel/config"
	"ssoo-utils/pcb"
	"sync"
	"time"
)

var (
	NewQueue      []*Process = make([]*Process, 0)
	NewQueueMutex sync.Mutex

	ReadyQueue      []*Process = make([]*Process, 0)
	ReadyQueueMutex sync.Mutex

	SuspReadyQueue      []*Process = make([]*Process, 0)
	SuspReadyQueueMutex sync.Mutex

	SuspBlockedQueue      []*Process = make([]*Process, 0)
	SuspBlockedQueueMutex sync.Mutex

	ExitQueue      []*Process = make([]*Process, 0)
	ExitQueueMutex sync.Mutex

	BlockedQueue      []*Process = make([]*Process, 0)
	BlockedQueueMutex sync.Mutex

	ExecQueue      []*Process = make([]*Process, 0)
	ExecQueueMutex sync.Mutex

	//
	AvailableIOs []IOConnection
	AvIOmu       sync.Mutex

	AvailableCPUs []*CPUConnection = make([]*CPUConnection, 0)
	AvCPUmu       sync.Mutex

	CPUsSlots   []*CPUSlot = make([]*CPUSlot, 0)
	CPUsSlotsMu sync.Mutex

	MTSQueue   []*WaitingIO = make([]*WaitingIO, 0)
	MTSQueueMu sync.Mutex
	//

	SchedulerStatus string
	NextPID         uint = 1 // ?
	PIDMutex        sync.Mutex

	LTSEmpty = make(chan struct{})
	STSEmpty = make(chan struct{})
	MTSEmpty = make(chan struct{})

	AvailableCpu = make(chan struct{}, 1) // Esto me parece que esta de más
	PCBReceived  = make(chan struct{}, 1)
	LTSStopped   = make(chan struct{})

	RetryNew                      = make(chan struct{})
	RetrySuspReady                = make(chan struct{})
	WaitingForMemory              = make(chan struct{}, 1)
	WaitingNewProcessInReady      = make(chan struct{}) // ?
	WaitingForCPU            bool = false
	WaitingForRetry          bool = false
	WaitingInMTS             bool = false
)

type IOConnection struct {
	Name    string
	Handler chan IORequest
	Disp    bool
}

type IORequest struct {
	Pid   uint
	Timer int
}

type WaitingIO struct {
	Process     *Process
	IORequest   IORequest // TODO: Pasarlo a memoria dinamica
	IOAvailable chan struct{}
}

type CPUSlot struct {
	Cpu     *CPUConnection
	Process *Process // nil si no hay proceso asignado
}

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
	PCB            *pcb.PCB
	Path           string
	Size           int
	StartTime      time.Time // cuando entra a RUNNING
	LastRealBurst  float64   // en segundos
	EstimatedBurst float64   // estimación actual
}

func (p Process) GetPath() string { return config.Values.CodeFolder + "/" + p.Path }

func SendIORequest(pid uint, timer int, io *IOConnection) {
	io.Handler <- IORequest{Pid: pid, Timer: timer}
}
