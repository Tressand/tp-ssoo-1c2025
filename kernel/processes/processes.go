package processes

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
	"time"
)

func CreateProcess(path string, size int) {
	newProcess := globals.Process{
		PCB: pcb.Create(
			GetNextPID(),
			path,
		),
		Path: path,
		Size: size,
	}

	logger.RequiredLog(true, newProcess.PCB.GetPID(), "Se crea el proceso", map[string]string{"Estado": newProcess.PCB.GetState().String(), "Tamaño": fmt.Sprintf("%d KB", size)})

	QueueToNew(newProcess)
}

func QueueToNew(process globals.Process) {
	globals.NewQueueMutex.Lock()
	globals.NewQueue = append(globals.NewQueue, process)
	globals.NewQueueMutex.Unlock()

	if globals.SchedulerStatus == "START" {
		select {
		case globals.LTSEmpty <- struct{}{}:
			slog.Debug("Se desbloquea LTS que estaba bloqueado por no haber procesos para planificar")
		default:
			slog.Debug("LTS tenia procesos para planificar, se ignora")
		}
	}

	logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d se encola en NEW", process.PCB.GetPID()))
}

func SearchProcessWorking(id string) (*globals.Process, error) {
	for _, processWorking := range globals.ExecQueue {
		if processWorking.Cpu.ID == id {
			fmt.Printf("Proceso de pid %v encontrado", processWorking.Process.PCB.GetPID())
			return processWorking.Process, nil
		}
	}

	return nil, errors.New("proceso no encontrado")
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

	realBurst := time.Since(process.IniciarTiempo).Seconds()
	previousEstimate := process.EstimatedBurst

	newEstimate := config.Values.Alpha*realBurst + (1-config.Values.Alpha)*previousEstimate

	process.LastRealBurst = realBurst
	process.EstimatedBurst = newEstimate

	slog.Info(fmt.Sprintf("PID %d - Burst real: %.2fs - Estimada previa: %.2f - Nueva estimación: %.2f",
		process.PCB.GetPID(), realBurst, previousEstimate, newEstimate))
}

func InitializeProcessInMemory(pid uint, codePath string, size int) error {
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
