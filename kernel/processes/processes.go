package processes

import (
	"fmt"
	"log/slog"
	"net/http"
	api "ssoo-kernel/api"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	queue "ssoo-kernel/queues"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
	"time"
)

func CreateProcess(path string, size int) {
	process := newProcess(path, size)

	logger.RequiredLog(true, process.PCB.GetPID(), "Se crea el proceso", map[string]string{"Estado": process.PCB.GetState().String(), "Tamaño": fmt.Sprintf("%d KB", size)})

	handleNewProcess(process)
}

func handleNewProcess(process *globals.Process) {
	if !api.TryInitProcess(process) {
		queue.Enqueue(pcb.NEW, process)
	}

	if globals.SchedulerStatus == "STOP" {
		return
	}

	select {
	case globals.LTSEmpty <- struct{}{}:
		slog.Debug("se desbloquea LTS que estaba bloqueado por no haber procesos para planificar")
	default:
		slog.Debug("LTS tenia procesos para planificar, se ignora")
	}
}

func newProcess(path string, size int) *globals.Process {
	process := new(globals.Process)
	process.PCB = pcb.Create(GetNextPID(), path)
	process.Path = path
	process.Size = size
	process.LastRealBurst = 1
	process.EstimatedBurst = 1
	return process
}

func EnqueueToNew(process *globals.Process) {
	globals.NewQueueMutex.Lock()
	globals.NewQueue = append(globals.NewQueue, process)
	globals.NewQueueMutex.Unlock()

	logger.Instance.Info(fmt.Sprintf("el proceso con el pid %d se encola en NEW", process.PCB.GetPID()))
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
	case globals.RetrySuspReady <- struct{}{}:
		slog.Debug("Se intenta inicializar nuevamente un proceso en Susp.Ready")
	default:
		select {
		case globals.RetryNew <- struct{}{}:
			slog.Debug("Se intenta inicializar nuevamente un proceso en NEW")
		default:
			slog.Debug("No hay procesos esperando ser inicializados nuevamente")
		}
	}

}

func GetNextPID() uint {
	globals.PIDMutex.Lock()
	defer globals.PIDMutex.Unlock()
	pid := globals.NextPID
	globals.NextPID++
	return pid
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
