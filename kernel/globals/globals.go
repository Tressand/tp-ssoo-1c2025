package globals

import (
	"context"
	"fmt"
	"log/slog"
	"math"
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

	NextPID  uint = 0
	PIDMutex sync.Mutex

	LTSEmpty = make(chan struct{})
	STSEmpty = make(chan struct{})
	MTSEmpty = make(chan struct{})

	CpuAvailableSignal = make(chan struct{})

	LTSStopped = make(chan struct{})

	RetryInitialization = make(chan struct{})

	TotalProcessesCreated int = 0

	UnsuspendMutex sync.Mutex

	// Sending anything to this channel will shutdown the server.
	// The server will respond back on this same channel to confirm closing.
	ShutdownSignal     chan any = make(chan any)
	IOctx, CancelIOctx          = context.WithCancel(context.Background())
)

type IOConnection struct {
	Name    string
	IP      string
	Port    string
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
	DUMP_MEMORY bool          // si se debe hacer DUMP_MEMORY al desbloquear
	CancelTimer chan struct{} // canal para cancelar el timer
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
	LastRealBurst  int64     // en segundos
	EstimatedBurst int64     // estimación actual
	TimerRunning   bool      // si se ha iniciado el timer en mts
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
	for _, io := range AvailableIOs {
		http.Get("http://" + io.IP + ":" + io.Port + "/shutdown")
	}

	http.Get(kill_url(config.Values.IpMemory, config.Values.PortMemory))

	ShutdownSignal <- struct{}{}
	<-ShutdownSignal
	os.Exit(0)
}

func UpdateBurstEstimation(process *Process) {

	realBurst := time.Since(process.StartTime).Milliseconds()
	previousEstimate := process.EstimatedBurst
	alpha := config.Values.Alpha

	newEstimatefloat := alpha*float64(realBurst) + (1-alpha)*float64(previousEstimate)
	newEstimate := int64(math.Ceil(newEstimatefloat)) // redondeo hacia arriba

	process.LastRealBurst = realBurst
	process.EstimatedBurst = newEstimate

	if config.Values.SchedulerAlgorithm == "SJF" || config.Values.SchedulerAlgorithm == "SRT" {

		slog.Info(fmt.Sprintf("PID %d - Burst real: %dms - Estimada previa: %dms - Nueva estimación: %dms",
			process.PCB.GetPID(), realBurst, previousEstimate, newEstimate))
	}
}

func TiempoRestanteDeRafaga(process *Process) int64 {

	start := process.StartTime
	estimado := process.EstimatedBurst

	restante := estimado - time.Since(start).Milliseconds() //cuanto le resta

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

func UnlockLTS() {
	select {
	case LTSEmpty <- struct{}{}:
		slog.Debug("Desbloqueando LTS...")
	case RetryInitialization <- struct{}{}:
		slog.Debug("Intentando inicializar proceso bloqueado por falta de memoria...")
	default:
	}
}

func UnlockMTS() {
	select {
	case MTSEmpty <- struct{}{}:
		slog.Debug("Desbloqueando MTS...")
	default:
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
