package scheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	kernel_api "ssoo-kernel/api"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	processes "ssoo-kernel/processes"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
)

func LTS() {
	<-globals.LTSStopped
	globals.SchedulerStatus = "START"
	logger.Instance.Debug("El planificador iniciado")

	for {
		switch config.Values.ReadyIngressAlgorithm {
		case "FIFO":
			logger.Instance.Info("Planificando con FIFO!")

			var process *globals.Process
			var found bool = false

			globals.SuspReadyQueueMutex.Lock()
			if len(globals.SuspReadyQueue) != 0 {
				process = &globals.SuspReadyQueue[0]
				globals.SuspReadyQueue = globals.SuspReadyQueue[1:]
				found = true
				logger.Instance.Info("Se encontró un proceso en Susp Ready", "found", found)
			}
			globals.SuspReadyQueueMutex.Unlock()

			logger.Instance.Info("No hay procesos en SuspReadyQueue", "found", found)

			globals.NewQueueMutex.Lock()
			if !found && len(globals.NewQueue) != 0 {
				process = &globals.NewQueue[0]
				globals.NewQueue = globals.NewQueue[1:]
				found = true
				logger.Instance.Info("Se encontró un proceso en New", "found", found)
			}
			globals.NewQueueMutex.Unlock()

			if !found {
				logger.Instance.Info("No hay procesos en SuspReadyQueue o NewQueue. Se bloquea hasta que se agregen procesos nuevos", "found", found)
				globals.WaitingInLTS = true
				<-globals.LTSEmpty
				globals.WaitingInLTS = false
				continue
			}

			logger.Instance.Debug("Se intenta inicializar un proceso en LTS", "pid", process.PCB.GetPID())

			waitMemory := make(chan struct{})

			go func(p *globals.Process, w chan struct{}) {
				InitProcess(p)
				logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d se inicializo en Memoria", p.PCB.GetPID()))
				w <- struct{}{} // Desbloquea el canal waitMemory una vez que el proceso se inicializa
				logger.Instance.Debug("Se desbloquea el canal waitMemory luego de inicializar el proceso", "pid", p.PCB.GetPID())
			}(process, waitMemory)

			logger.Instance.Debug("Se bloquea el canal waitMemory hasta que se inicialice el proceso")
			<-waitMemory // ?
			logger.Instance.Debug("Se desbloquea el canal waitMemory")

		case "PMCP":
			var process *globals.Process
			var found bool = false

			globals.SuspReadyQueueMutex.Lock()
			if len(globals.SuspReadyQueue) != 0 {

				sort.Slice(globals.SuspReadyQueue, func(i, j int) bool {
					return globals.SuspReadyQueue[i].Size < globals.SuspReadyQueue[j].Size
				})

				process = &globals.SuspReadyQueue[0]
				globals.SuspReadyQueue = globals.SuspReadyQueue[1:]
				found = true
			}
			globals.SuspReadyQueueMutex.Unlock()

			globals.NewQueueMutex.Lock()
			if !found && len(globals.NewQueue) != 0 {

				sort.Slice(globals.NewQueue, func(i, j int) bool {
					return globals.NewQueue[i].Size < globals.NewQueue[j].Size
				})

				process = &globals.NewQueue[0]
				globals.NewQueue = globals.NewQueue[1:]
				found = true
			}
			globals.NewQueueMutex.Unlock()

			if !found {
				globals.WaitingInLTS = true
				<-globals.LTSEmpty
				globals.WaitingInLTS = false
				continue
			}

			waitMemory := make(chan struct{}, 1)

			go func(p *globals.Process, w chan struct{}) {
				InitProcess(p)
				logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d se inicializo en Memoria", p.PCB.GetPID()))
				w <- struct{}{} // Desbloquea el canal waitMemory una vez que el proceso se inicializa
			}(process, waitMemory)

			<-waitMemory

		default:
			fmt.Fprintf(os.Stderr, "Algorithm not supported - %s\n", config.Values.ReadyIngressAlgorithm)
			return
		}
	}
}

func InitProcess(process *globals.Process) {
	for {
		slog.Info("Intentando inicializar proceso", "name", process.PCB.GetPID())
		err := processes.InitializeProcessInMemory(process.PCB.GetPID(), process.GetPath(), process.Size)

		if err == nil {
			slog.Info("Proceso inicializado en memoria", "name", process.PCB.GetPID())
			QueueToReady(process)
			globals.ProcessWaiting = false
			logger.Instance.Debug("Se desbloquea el canal waitMemory luego de inicializar el proceso", "pid", process.PCB.GetPID())
			return
		} else {
			slog.Error(err.Error())
			slog.Info("Proceso entra en espera. Memoria no pudo inicializarlo", "name", process.PCB.GetPID())
			globals.ProcessWaiting = true
			<-globals.RetryProcessCh // Este espera ser desbloqueado desde Finalización de Proceso
		}
		slog.Info("Reintentando inicializar proceso", "name", process.PCB.GetPID())
	}
}

func QueueToReady(process *globals.Process) {
	lastState := process.PCB.GetState()
	process.PCB.SetState(pcb.READY)
	actualState := process.PCB.GetState()

	globals.ReadyQueueMutex.Lock()
	globals.ReadyQueue = append(globals.ReadyQueue, *process)
	if len(globals.ReadyQueue) != 0 && globals.WaitingProcessInReady {
		slog.Info("Desbloqueando STS porque hay procesos en READY")
		globals.STSEmpty <- struct{}{}
	}
	globals.ReadyQueueMutex.Unlock()

	logger.RequiredLog(true, process.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})
}

func STS() {
	for {
		var cpu *globals.CPUConnection

		if len(kernel_api.GetCPUList(false)) == 0 {
			globals.WaitingForCPU = true
			<-globals.AvailableCpu
			continue
		} else {
			globals.WaitingForCPU = false
			cpu = kernel_api.GetCPUList(false)[0]
		}

		switch config.Values.SchedulerAlgorithm {
		case "FIFO":
			var process *globals.Process
			var found bool = false // ?

			globals.ReadyQueueMutex.Lock()
			if len(globals.ReadyQueue) != 0 {
				process = &globals.ReadyQueue[0]
				globals.ReadyQueue = globals.ReadyQueue[1:]
				found = true
			}
			globals.ReadyQueueMutex.Unlock()

			if !found {
				globals.WaitingProcessInReady = true
				slog.Info("Me estoy bloqueando en STS porque no hay procesos en READY")
				<-globals.STSEmpty
				globals.WaitingProcessInReady = false
				slog.Info("Me desbloqueo en STS porque hay procesos en READY")
				continue
			}

			go func(p *globals.Process, c *globals.CPUConnection) {
				sendToExecute(p, c)
			}(process, cpu)

		case "SJF":
			fmt.Println("SJF")
			return
		case "SRT":
			fmt.Println("SRT")
			return
		default:
			fmt.Fprintf(os.Stderr, "Algorithm not supported - %s\n", config.Values.SchedulerAlgorithm)
			return
		}
	}
}

func sendToExecute(process *globals.Process, cpu *globals.CPUConnection) {

	lastState := process.PCB.GetState()
	process.PCB.SetState(pcb.EXEC)
	actualState := process.PCB.GetState()
	logger.RequiredLog(true, process.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})

	changeAvailableCpu(cpu, true)

	logger.Instance.Debug("cpusAvailable", "available", globals.AvailableCPUs)

	globals.ExecQueueMutex.Lock()
	globals.ExecQueue = append(globals.ExecQueue, globals.CPUSlot{
		Cpu:     cpu,
		Process: process,
	})
	globals.ExecQueueMutex.Unlock()

	request := globals.CPURequest{
		PID: process.PCB.GetPID(),
		PC:  process.PCB.GetPC(),
	}

	dispatchResp, err := sendToWork(*cpu, request)

	if err != nil {
		logger.Instance.Error("Error al enviar el proceso a la CPU", "error", err)
		return
	}

	switch dispatchResp.Motivo {
	case "Interrupt":
		logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d fue interrumpido por la CPU", dispatchResp.PID))

		QueueToReady(process)
		changeAvailableCpu(cpu, false)
		return
	case "Exit":
		logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d fue finalizado por la CPU", dispatchResp.PID))
		return
	default:
		logger.Instance.Error("Motivo desconocido", "Motivo", dispatchResp.Motivo)
		return
	}

}

func changeAvailableCpu(cpu *globals.CPUConnection, working bool) {
	globals.CpuListMutex.Lock()
	defer globals.CpuListMutex.Unlock()
	for i := range globals.AvailableCPUs {
		if globals.AvailableCPUs[i].ID == cpu.ID {
			globals.AvailableCPUs[i].Working = working
			break
		}
	}
}

func sendToWork(cpu globals.CPUConnection, request globals.CPURequest) (globals.DispatchResponse, error) {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       cpu.IP,
		Port:     cpu.Port,
		Endpoint: "dispatch",
	})

	jsonRequest, err := json.Marshal(request)
	if err != nil {
		logger.Instance.Error("Error marshaling request to JSON", "error", err)
		return globals.DispatchResponse{}, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(jsonRequest))
	if err != nil {
		logger.Instance.Error("Error making POST request", "error", err)
		return globals.DispatchResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTeapot {
			logger.Instance.Warn("Server requested shutdown")
			return globals.DispatchResponse{}, fmt.Errorf("server asked for shutdown")
		}
		logger.Instance.Error("Unexpected status code from CPU", "status", resp.StatusCode)
		return globals.DispatchResponse{}, fmt.Errorf("unexpected response status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Instance.Error("Error reading response body", "error", err)
		return globals.DispatchResponse{}, err
	}

	var dispatchResp globals.DispatchResponse
	err = json.Unmarshal(data, &dispatchResp)
	if err != nil {
		logger.Instance.Error("Error unmarshaling response", "error", err)
		return globals.DispatchResponse{}, err
	}

	return dispatchResp, nil
}


