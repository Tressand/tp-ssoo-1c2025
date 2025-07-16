package globals

import (
	"context"
	"fmt"
	"net/http"
	"os"
	config "ssoo-kernel/config"
	"ssoo-utils/httputils"
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

	AvailableIOs []*IOConnection = make([]*IOConnection, 0)
	AvIOmu       sync.Mutex

	AvailableCPUs []*CPUConnection = make([]*CPUConnection, 0)
	AvCPUmu       sync.Mutex

	MTSQueue   []*Blocked = make([]*Blocked, 0)
	MTSQueueMu sync.Mutex

	NextPID  uint = 1
	PIDMutex sync.Mutex

	BlockedForMemory = make(chan struct{})

	LTSEmpty = make(chan struct{})
	STSEmpty = make(chan struct{})
	MTSEmpty = make(chan struct{})

	CpuAvailableSignal = make(chan struct{})

	LTSStopped = make(chan struct{})

	RetryInitialization = make(chan struct{})

	WaitingForRetry       bool = false
	WaitingForRetryMu     sync.Mutex
	TotalProcessesCreated int = 0

	// Sending anything to this channel will shutdown the server.
	// The server will respond back on this same channel to confirm closing.
	ShutdownSignal     chan any = make(chan any)
	IOctx, CancelIOctx          = context.WithCancel(context.Background())
)

type IOConnection struct {
	Name    string
	IP      string
	Port    int
	Handler chan IORequest
	Disp    bool
}

type IORequest struct {
	Pid   uint
	Timer int
}

type Blocked struct {
	Process *Process
	Name    string
	Time    int
}

type CPUConnection struct {
	ID      string
	IP      string
	Port    int
	Process *Process
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
	TimerStarted   bool      // si se ha iniciado el timer en mts
}

var ReadySuspended = false

func (p Process) GetPath() string { return config.Values.CodeFolder + "/" + p.Path }

func SendIORequest(pid uint, timer int, io *IOConnection) {
	io.Handler <- IORequest{Pid: pid, Timer: timer}
}

func ClearAndExit() {
	fmt.Println("Cerrando Kernel...")

	CancelIOctx()

	kill_url := func(ip string, port int) string {
		return httputils.BuildUrl(httputils.URLData{
			Ip:       ip,
			Port:     port,
			Endpoint: "shutdown",
		})
	}

	for _, cpu := range AvailableCPUs {
		http.Get(kill_url(cpu.IP, cpu.Port))
	}

	http.Get(kill_url(config.Values.IpMemory, config.Values.PortMemory))

	ShutdownSignal <- struct{}{}
	<-ShutdownSignal
	os.Exit(0)
}
