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
)

func CreateProcess(path string, size int) {
	process := newProcess(path, size)

	slog.Info("Se crea el proceso", "pid", process.PCB.GetPID(), "path", path, "size", size)

	HandleNewProcess(process)
}

func newProcess(path string, size int) *globals.Process {
	process := new(globals.Process)
	process.PCB = pcb.Create(getNextPID(), path)
	process.Path = path
	process.Size = size
	process.LastRealBurst = 1
	process.EstimatedBurst = 1
	return process
}

func GetCPUList(working bool) []*globals.CPUConnection {
	result := make([]*globals.CPUConnection, 0)
	for i := range globals.AvailableCPUs {
		if globals.AvailableCPUs[i].Working == working {
			result = append(result, globals.AvailableCPUs[i])
		}
	}
	return result
}

func sendToInitializeInMemory(pid uint, codePath string, size int) error {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "process",
		Queries: map[string]string{
			"pid": fmt.Sprint(pid),
			"req": fmt.Sprint(size),
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
		slog.Info(fmt.Sprintf("El proceso con el pid %d se inicializó en Memoria", process.PCB.GetPID()))

		queue.Enqueue(pcb.READY, process)

		select {
		case globals.STSEmpty <- struct{}{}:
			slog.Debug("Desbloqueando STS porque hay procesos en READY")
		default:
			select {
			case globals.WaitingNewProcessInReady <- struct{}{}:
				slog.Debug("Replanificando STS")
			default:
			}
		}
		return true
	}
	slog.Info(fmt.Sprintf("No se pudo inicializar el proceso con el pid %d en Memoria", process.PCB.GetPID()))
	return false
}

func InititializeProcess(process *globals.Process) {
	for {
		if !TryInititializeProcess(process) {
			slog.Info("Proceso entra en espera. Memoria no pudo inicializarlo", "pid", process.PCB.GetPID())

			if process.PCB.GetState() == pcb.SUSP_READY { // ? Sera necesario distinguir entre SUSP_READY y NEW?
				<-globals.RetrySuspReady
			} else {
				<-globals.RetryNew
			}

			slog.Info("Reintentando inicializar proceso", "name", process.PCB.GetPID())
		}
	}
}

func HandleNewProcess(process *globals.Process) {

	//	if !TryInititializeProcess(process) {
	queue.Enqueue(pcb.NEW, process)
	//	}

	if globals.SchedulerStatus == "STOP" {
		return
	}

	select {
	case globals.LTSEmpty <- struct{}{}:
		slog.Debug("se desbloquea LTS que estaba bloqueado por no haber procesos para planificar")
	default:
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

func getNextPID() uint {
	globals.PIDMutex.Lock()
	defer globals.PIDMutex.Unlock()
	pid := globals.NextPID
	globals.NextPID++
	return pid
}
