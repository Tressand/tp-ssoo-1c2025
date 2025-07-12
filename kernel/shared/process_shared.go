package shared

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"ssoo-kernel/config"
	"ssoo-kernel/globals"
	queue "ssoo-kernel/queues"
	"ssoo-utils/httputils"
	logger "ssoo-utils/logger"
	"ssoo-utils/pcb"
	"time"
)

func CreateProcess(path string, size int) {
	process := newProcess(path, size)

	slog.Info("Se crea el proceso", "pid", process.PCB.GetPID(), "path", path, "size", size)

	go HandleNewProcess(process) // ????
}

func UpdateBurstEstimation(process *globals.Process) {

	realBurst := time.Since(process.StartTime).Seconds()
	previousEstimate := process.EstimatedBurst

	newEstimate := config.Values.Alpha*realBurst + (1-config.Values.Alpha)*previousEstimate

	process.LastRealBurst = realBurst
	process.EstimatedBurst = newEstimate

	slog.Info(fmt.Sprintf("PID %d - Burst real: %.2fs - Estimada previa: %.2f - Nueva estimación: %.2f",
		process.PCB.GetPID(), realBurst, previousEstimate, newEstimate))
}

func newProcess(path string, size int) *globals.Process {
	process := new(globals.Process)
	process.PCB = pcb.Create(getNextPID(), path)
	process.Path = path
	process.Size = size
	process.LastRealBurst = 0
	process.EstimatedBurst = float64(config.Values.InitialEstimate) / 1000.0

	return process
}

func sendToInitializeInMemory(pid uint, codePath string, size int) error {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "process",
		Queries: map[string]string{
			"pid":  fmt.Sprint(pid),
			"size": fmt.Sprint(size),
		},
	})

	codeFile, err := os.OpenFile(codePath, os.O_RDONLY, 0666)
	if err != nil {
		return fmt.Errorf("error al abrir el archivo de código: %v", err)
	}

	resp, err := http.Post(url, "text/plain", codeFile)
	if err != nil {
		return fmt.Errorf("error al llamar a Memoria: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("memoria rechazó la creación (código %d)", resp.StatusCode)
	}

	return nil
}

func TryInititializeProcess(process *globals.Process) bool {
	err := sendToInitializeInMemory(process.PCB.GetPID(), process.GetPath(), process.Size)

	if err == nil {
		slog.Info("Se inicializo en memoria el proceso", "pid", process.PCB.GetPID(), "path", process.GetPath(), "size", process.Size)

		queue.Enqueue(pcb.READY, process)

		select {
		case globals.STSEmpty <- struct{}{}:
			slog.Debug("Desbloqueando STS porque hay procesos en READY")
		default:
		}

		select {
		case globals.NewProcessInReadySignal <- struct{}{}:
			slog.Debug("Replanificando STS")
		default:
		}

		return true
	}

	slog.Info("No se pudo inicializar el proceso en Memoria", "pid", process.PCB.GetPID(), "error", err.Error())
	return false
}

func InititializeProcess(process *globals.Process) bool {
	initialized := TryInititializeProcess(process)

	if globals.WaitingForRetry || initialized {
		return initialized
	}

	for {
		slog.Info("Proceso entra en espera. Memoria no pudo inicializarlo", "pid", process.PCB.GetPID())
		<-globals.RetryInitialization // TODO: buffer

		initialized = TryInititializeProcess(process)

		if initialized {
			globals.WaitingForRetryMu.Lock()
			globals.WaitingForRetry = true
			globals.WaitingForRetryMu.Unlock()
			break
		}
	}

	select {
	case globals.BlockedForMemory <- struct{}{}:
		slog.Debug("Se desbloquea LTS que estaba bloqueado porque habia un proceso esperando para inicializarse")
	default:
	}

	return true
}

func isSmallerThanAll(process *globals.Process) bool {
	if len(globals.NewQueue) == 0 {
		slog.Debug("No hay procesos en NEW, se puede inicializar directamente")
		return true
	}

	for _, p := range globals.NewQueue {
		if p.Size < process.Size {
			return false
		}
	}
	return true
}

func SuspReadyIsEmpty() bool {
	globals.SuspReadyQueueMutex.Lock()
	defer globals.SuspReadyQueueMutex.Unlock()
	return len(globals.SuspReadyQueue) == 0
}

func NewIsEmpty() bool {
	globals.NewQueueMutex.Lock()
	defer globals.NewQueueMutex.Unlock()
	return len(globals.NewQueue) == 0
}

func HandleNewProcess(process *globals.Process) {
	initialized := false

	if shouldTryInitialize(process) && SuspReadyIsEmpty() {
		initialized = InititializeProcess(process)
	}

	if !initialized {
		queue.Enqueue(pcb.NEW, process)
		notifyNewProcessInNew()
	}

}

func notifyNewProcessInNew() {
	select {
	case globals.LTSEmpty <- struct{}{}:
		slog.Debug("se desbloquea LTS que estaba bloqueado por no haber procesos para planificar")
	default:
	}
}

func shouldTryInitialize(process *globals.Process) bool {
	switch config.Values.ReadyIngressAlgorithm {
	case "FIFO":
		return !globals.WaitingForRetry && NewIsEmpty()
	case "PMCP":
		return isSmallerThanAll(process)
	default:
		slog.Error("Algoritmo de ingreso a READY no soportado", "algoritmo", config.Values.ReadyIngressAlgorithm)
		return false
	}
}

func TerminateProcess(process *globals.Process) {
	pid := process.PCB.GetPID()
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "process",
		Queries: map[string]string{
			"pid": fmt.Sprint(pid),
		},
	})

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		logger.RequiredLog(true, pid, "Error creando request DELETE", map[string]string{"Error": err.Error()})
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.RequiredLog(true, pid, "Error al eliminar el proceso de memoria", map[string]string{"Error": err.Error()})
		return
	}

	if resp.StatusCode != http.StatusOK {
		logger.RequiredLog(true, pid, "Error al eliminar el proceso de memoria", map[string]string{"Código": fmt.Sprint(resp.StatusCode)})
		return
	}

	defer resp.Body.Close()

	logger.RequiredLog(true, pid, "", map[string]string{"Métricas de estado:": process.PCB.GetKernelMetrics().String()})

	select {
	case globals.RetryInitialization <- struct{}{}:
		slog.Debug("Se libera memoria y hay procesos esperando para inicializarse. Se envia signal de reintento")
	default:
	}
}

func getNextPID() uint {
	globals.PIDMutex.Lock()
	defer globals.PIDMutex.Unlock()
	pid := globals.NextPID
	globals.NextPID++
	return pid
}
