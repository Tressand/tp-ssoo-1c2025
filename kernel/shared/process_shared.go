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
	"strconv"
)

func CreateProcess(path string, size int) {
	process := newProcess(path, size)
	globals.TotalProcessesCreated++

	logger.RequiredLog(true, process.PCB.GetPID(), "Se crea el proceso",
		map[string]string{
			"Estado": "NEW",
			"Path":   path,
			"Size":   fmt.Sprintf("%d bytes", size),
		})

	HandleNewProcess(process)
}

func newProcess(path string, size int) *globals.Process {
	process := new(globals.Process)
	process.PCB = pcb.Create(getNextPID(), path)
	process.Path = path
	process.Size = size
	process.LastRealBurst = 0
	process.EstimatedBurst = config.Values.InitialEstimate
	process.TimerRunning = false
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

	queues.RemoveByPID(pcb.NEW, process.PCB.GetPID())
	queues.Enqueue(pcb.READY, process)

	select {
	case globals.STSEmpty <- struct{}{}:
	default:
	}

	return true
}

func HandleNewProcess(process *globals.Process) {
	queues.Enqueue(pcb.NEW, process)

	select {
	case globals.LTSEmpty <- struct{}{}:
		slog.Debug("se desbloquea LTS que estaba bloqueado por no haber procesos para planificar")
	default:
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
	queues.MostrarLasColas("TerminateProcess")

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

func Unsuspend(process *globals.Process) bool {

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
