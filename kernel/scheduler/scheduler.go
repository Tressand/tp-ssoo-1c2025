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
	"strconv"
	"time"
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
				<-globals.LTSEmpty
				continue
			}

			logger.Instance.Debug("Se intenta inicializar un proceso en LTS", "pid", process.PCB.GetPID())

			InitProcess(process) // ! Revisar esto!!!!!!!!
			logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d se inicializo en Memoria", process.PCB.GetPID()))

		case "PMCP":
			logger.Instance.Info("Planificando con PMCP!")

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
				logger.Instance.Info("Se encontró un proceso en Susp Ready", "found", found)
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
				logger.Instance.Info("Se encontró un proceso en New", "found", found)
			}
			globals.NewQueueMutex.Unlock()

			if !found {
				logger.Instance.Info("No hay procesos en SuspReadyQueue o NewQueue. Se bloquea hasta que se agregen procesos nuevos", "found", found)
				<-globals.LTSEmpty
				continue
			}

			InitProcess(process) // ! Revisar esto!!!!!!!!

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
			slog.Info(fmt.Sprintf("El proceso con el pid %d se inicializó en Memoria", process.PCB.GetPID()))
			QueueToReady(process)
			return
		} else {
			slog.Error(err.Error())
			slog.Info("Proceso entra en espera. Memoria no pudo inicializarlo", "name", process.PCB.GetPID())

			if process.PCB.GetState() == pcb.SUSP_READY {
				<-globals.RetrySuspReady
			} else {
				<-globals.RetryNew
			}
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
	globals.ReadyQueueMutex.Unlock()

	logger.RequiredLog(true, process.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})
}

func STS() {
	slog.Debug("STS Desbloqueado")
	for {
		slog.Debug("STS Desbloqueado")
		switch config.Values.SchedulerAlgorithm {
		case "FIFO":
			var cpu *globals.CPUConnection

			globals.CpuListMutex.Lock()
			cpus := kernel_api.GetCPUList(false)

			if len(cpus) == 0 {
				globals.CpuListMutex.Unlock()
				<-globals.AvailableCpu
				continue
			}

			cpu = cpus[0]

			globals.CpuListMutex.Unlock()

			var process *globals.Process
			var found bool = false

			globals.ReadyQueueMutex.Lock()
			if len(globals.ReadyQueue) != 0 {
				process = &globals.ReadyQueue[0]
				globals.ReadyQueue = globals.ReadyQueue[1:]
				found = true
			}
			globals.ReadyQueueMutex.Unlock()

			if !found {
				slog.Info("Me estoy bloqueando en STS porque no hay procesos en READY")
				<-globals.STSEmpty
				slog.Info("Me desbloqueo en STS porque hay procesos en READY")
				continue
			}

			go sendToExecute(process, cpu)

		case "SJF":
			SJF()
		case "SRT":
			SRT()
		default:
			fmt.Fprintf(os.Stderr, "Algorithm not supported - %s\n", config.Values.SchedulerAlgorithm)
			return
		}
	}
}

func SJF() {
	var cpu *globals.CPUConnection

	globals.CpuListMutex.Lock()
	cpus := kernel_api.GetCPUList(false)
	globals.CpuListMutex.Unlock()

	if len(cpus) == 0 {
		<-globals.AvailableCpu
		return
	}

	globals.CpuListMutex.Lock()
	cpu = kernel_api.GetCPUList(false)[0]
	globals.CpuListMutex.Unlock()

	var process *globals.Process
	var found bool = false

	slog.Debug("No me bloquie en las cpus")

	globals.ReadyQueueMutex.Lock()
	if len(globals.ReadyQueue) != 0 {
		minIndex := 0
		for i := 1; i < len(globals.ReadyQueue); i++ {
			if globals.ReadyQueue[i].EstimatedBurst < globals.ReadyQueue[minIndex].EstimatedBurst {
				minIndex = i
			}
		}
		process = &globals.ReadyQueue[minIndex]
		found = true
		globals.ReadyQueue = append(globals.ReadyQueue[:minIndex], globals.ReadyQueue[minIndex+1:]...)
	}
	globals.ReadyQueueMutex.Unlock()

	slog.Debug("Me bloqueo antes del !found????")

	if !found {
		slog.Info("Me estoy bloqueando en STS porque no hay procesos en READY")
		<-globals.STSEmpty
		slog.Info("Me desbloqueo en STS porque hay procesos en READY")
		return
	}

	slog.Debug("No me bloquie antes del found!")

	slog.Debug("Intento enviar a ejecutar un proceso....")

	go sendToExecute(process, cpu)
}

func SRT() {
	slog.Debug("Entre a SRT")

	globals.CpuListMutex.Lock()
	cpus := kernel_api.GetCPUList(false)
	globals.CpuListMutex.Unlock()

	slog.Debug("CPUs disponibles", "cpus", cpus)

	if len(cpus) != 0 {
		slog.Debug("Estoy en SRT, entrando a SJF...")
		SJF()
		return
	}

	slog.Debug("Utilizando la logica especifica de SRT")

	var process *globals.Process
	var found bool = false

	globals.ReadyQueueMutex.Lock()
	if len(globals.ReadyQueue) != 0 {
		minIndex := 0
		for i := 1; i < len(globals.ReadyQueue); i++ {
			if globals.ReadyQueue[i].EstimatedBurst < globals.ReadyQueue[minIndex].EstimatedBurst {
				minIndex = i
			}
		}
		process = &globals.ReadyQueue[minIndex]
		found = true
		globals.ReadyQueue = append(globals.ReadyQueue[:minIndex], globals.ReadyQueue[minIndex+1:]...)
	}
	globals.ReadyQueueMutex.Unlock()

	slog.Debug("Me bloquie antes del !found")

	if !found {
		slog.Info("Me estoy bloqueando en STS porque no hay procesos en READY")
		<-globals.STSEmpty
		slog.Info("Me desbloqueo en STS porque hay procesos en READY")
		return
	}

	var forInterrupt bool = false
	var toInterrupt *globals.CPUSlot

	globals.ExecQueueMutex.Lock()

	if len(globals.ExecQueue) != 0 {
		minBurst := globals.ExecQueue[0].Process.EstimatedBurst

		for i := range globals.ExecQueue {
			slot := &globals.ExecQueue[i]
			if slot.Process.EstimatedBurst > process.EstimatedBurst && slot.Process.EstimatedBurst < minBurst {
				minBurst = slot.Process.EstimatedBurst
				toInterrupt = slot
				forInterrupt = true
			}
		}
	}

	globals.ExecQueueMutex.Unlock()

	slog.Debug("Me bloquie antes forInterrupt")

	if forInterrupt {
		slog.Debug("No entre a !!")
		err := interruptCPU(*toInterrupt.Cpu, int(toInterrupt.Process.PCB.GetPID()))

		if err != nil {
			slog.Error("Error al interrumpir proceso", "pid", toInterrupt.Process.PCB.GetPID(), "error", err)

			globals.ReadyQueueMutex.Lock()
			globals.ReadyQueue = append(globals.ReadyQueue, *process)
			globals.ReadyQueueMutex.Unlock()

			return
		}

		slog.Info("Desalojo ejecutado correctamente", "pid", toInterrupt.Process.PCB.GetPID())

		go sendToExecute(process, toInterrupt.Cpu)
		return
	}

	globals.ReadyQueueMutex.Lock()
	globals.ReadyQueue = append(globals.ReadyQueue, *process)
	globals.ReadyQueueMutex.Unlock()

	<-globals.WaitingNewProcessInReady
}

func interruptCPU(cpu globals.CPUConnection, pid int) error {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       cpu.IP,
		Port:     cpu.Port,
		Endpoint: "interrupt",
	})

	// El PID va como texto plano
	body := bytes.NewReader([]byte(strconv.Itoa(pid)))

	resp, err := http.Post(url, "text/plain", body)
	if err != nil {
		logger.Instance.Error("Error enviando interrupción a CPU", "ip", cpu.IP, "port", cpu.Port, "pid", pid, "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Instance.Error("CPU respondió con error a la interrupción", "status", resp.StatusCode, "ip", cpu.IP, "port", cpu.Port, "pid", pid)
		return fmt.Errorf("interrupción fallida: status code %d", resp.StatusCode)
	}

	logger.Instance.Info("Interrupción enviada correctamente", "ip", cpu.IP, "port", cpu.Port, "pid", pid)
	return nil
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

	process.IniciarTiempo = time.Now()

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
		processes.UpdateBurstEstimation(process)
		QueueToReady(process)
		changeAvailableCpu(cpu, false)
		return
	case "Exit":
		processes.UpdateBurstEstimation(process)
		logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d fue finalizado por la CPU", dispatchResp.PID))

		select {
		case globals.WaitingNewProcessInReady <- struct{}{}:
			slog.Debug("Replanificando STS")
		default:
		}

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
