package globals

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"ssoo-kernel/config"
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
	ID      string
	Handler chan IORequest
	Disp    bool
}

type IORequest struct {
	Pid   uint
	Timer int
}

type Blocked struct {
	Process     *Process
	Name        string
	Time        int
	Working     bool
	DUMP_MEMORY bool // si se debe hacer DUMP_MEMORY al desbloquear
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
	InMemory       bool      // si el proceso está en memoria
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

func TiempoRestanteDeRafaga(process *Process) float64 {

	start := process.StartTime
	estimado := process.EstimatedBurst
	real := process.LastRealBurst
	alpha := config.Values.InitialEstimate

	siguiente := alpha*real + (1-alpha)*estimado

	restante := siguiente - float64(time.Since(start).Milliseconds()) //cuanto le resta

	if restante < 0 {
		return 0
	}
	return restante
}

func MayorTiempoRestanteDeRafaga(procesos []*Process) *Process {
	if len(procesos) == 0 {
		return nil
	}

	maxProcess := procesos[0]
	maxTime := TiempoRestanteDeRafaga(maxProcess)

	for _, process := range procesos[1:] {
		tiempoRestante := TiempoRestanteDeRafaga(process)
		if tiempoRestante > maxTime {
			maxTime = tiempoRestante
			maxProcess = process
		}
	}

	return maxProcess
}

func MenorTiempoRestanteDeRafaga(procesos []*Process) *Process {
	if len(procesos) == 0 {
		return nil
	}

	maxProcess := procesos[0]
	maxTime := TiempoRestanteDeRafaga(maxProcess)

	for _, process := range procesos[1:] {
		tiempoRestante := TiempoRestanteDeRafaga(process)
		if tiempoRestante > maxTime {
			maxTime = tiempoRestante
			maxProcess = process
		}
	}

	return maxProcess
}

func UnlockSTS() {
	select {
	case STSEmpty <- struct{}{}:
		slog.Debug("Desbloqueando STS porque hay procesos en READY")
	default:
		slog.Debug("STS ya desbloqueado, no se envía señal")
	}
	select {
	case CpuAvailableSignal <- struct{}{}:
		slog.Debug("Desbloqueando STS porque hay procesos en READY")
	default:
		slog.Debug("STS ya desbloqueado, no se envía señal")
	}
}

// removeBlockedByPID removes a blocked process from globals.MTSQueue by PID.
func RemoveBlockedByPID(pid uint) {
	for i, blocked := range MTSQueue {
		if blocked.Process.PCB.GetPID() == pid {
			MTSQueueMu.Lock()
			MTSQueue = append(MTSQueue[:i], MTSQueue[i+1:]...)
			MTSQueueMu.Unlock()
			return
		}
	}
}
