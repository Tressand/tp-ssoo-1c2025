package shared

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"ssoo-kernel/config"
	"ssoo-kernel/globals"
	"ssoo-kernel/queues"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
	"time"
	"strconv"
)

func CreateProcess(path string, size int) {
	process := newProcess(path, size)
	globals.TotalProcessesCreated++

	logger.RequiredLog(true,process.PCB.GetPID(),"Se crea el proceso",
		map[string]string{
			"Estado": "NEW",
			"Path":   path,
			"Size":   fmt.Sprintf("%d bytes", size),
		})

	HandleNewProcess(process)
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
	process.TimerStarted = false
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
	if err != nil {
		return false
	}

	queues.Enqueue(pcb.READY, process)

	select {
	case globals.STSEmpty <- struct{}{}:
	default:
	}

	return true
}

func InititializeProcess(process *globals.Process) {
	initialized := TryInititializeProcess(process)
	logger.RequiredLog(true, process.PCB.GetPID(), "Se crea el proceso", map[string]string{"Estado": "NEW"})

	if initialized {
		return
	}

	for {
		slog.Info("Proceso entra en espera. Memoria no pudo inicializarlo", "pid", process.PCB.GetPID())
		<-globals.RetryInitialization

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

func HandleNewProcess(process *globals.Process) {
	initialized := false

	if shouldTryInitialize(process) && queues.IsEmpty(pcb.SUSP_READY) {
		initialized = TryInititializeProcess(process)
	}

	if !initialized {
		queues.Enqueue(pcb.NEW, process)
		select {
		case globals.LTSEmpty <- struct{}{}:
			slog.Debug("se desbloquea LTS que estaba bloqueado por no haber procesos para planificar")
		default:
		}
	}
}

func shouldTryInitialize(process *globals.Process) bool {
	switch config.Values.ReadyIngressAlgorithm {
	case "PMCP":
		return globals.WaitingForRetry && isSmallerThanAll(process)
	default:
		return false
	}
}

func TerminateProcess(process *globals.Process) {
	if len(globals.ExitQueue) == globals.TotalProcessesCreated {
		defer globals.ClearAndExit()
	}

	pid := process.PCB.GetPID()
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "process",
		Queries: map[string]string{
			"pid": fmt.Sprint(pid),
		},
	})

	req, _ := http.NewRequest(http.MethodDelete, url, nil)
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
	queues.MostrarLasColas()

	select {
	case globals.MTSEmpty <- struct{}{}:
		slog.Debug("Se libera memoria y hay procesos esperando para planificar. Se envia signal de desbloqueo de LTS")
	default:
		slog.Debug("No hay procesos esperando para inicializarse, ni tampoco en Suspendido Ready.")
	}
}

func getNextPID() uint {
	globals.PIDMutex.Lock()
	pid := globals.NextPID
	globals.NextPID++
	globals.PIDMutex.Unlock()
	return pid
}

func Unsuspend(process *globals.Process) bool{

	slog.Debug("Desbloqueando proceso", "pid", process.PCB.GetPID())

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "unsuspend",
		Queries: map[string]string{
			"pid": strconv.Itoa(int(process.PCB.GetPID())),
		},
	})

	resp, err := http.Post(url, "text/plain", nil)
	if err != nil {
		logger.Instance.Error("Error al enviar solicitud de unsuspend", "pid", process.PCB.GetPID(), "error", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Instance.Error("Memoria rechazó la solicitud de unsuspend", "pid", process.PCB.GetPID(), "status", resp.StatusCode)
		return false 
	}

	process.InMemory = true
	slog.Info("Solicitud de unsuspend enviada correctamente", "pid", process.PCB.GetPID())

	return true
}