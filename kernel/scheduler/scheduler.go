package scheduler

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	kernel_api "ssoo-kernel/api"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	processes "ssoo-kernel/processes"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
)

func LTS() {
	for {
		switch config.Values.ReadyIngressAlgorithm {
		case "FIFO":
			var process globals.Process
			var found bool = false

			globals.SuspReadyQueueMutex.Lock()
			if len(globals.SuspReadyQueue) != 0 {
				process = globals.SuspReadyQueue[0]
				globals.SuspReadyQueue = globals.SuspReadyQueue[1:]
				found = true
			}
			globals.SuspReadyQueueMutex.Unlock()

			globals.NewQueueMutex.Lock()
			if !found && len(globals.NewQueue) != 0 {
				process = globals.NewQueue[0]
				globals.NewQueue = globals.NewQueue[1:]
				found = true
			}
			globals.NewQueueMutex.Lock()

			if !found {
				<-globals.LTSEmpty
				continue
			}

			go func(p *globals.Process) {
				InitProcess(p)
				logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d se inicializo en Memoria", p.PCB.GetPID()))
				globals.WaitingForMemory <- struct{}{}
			}(&process)

			<-globals.WaitingForMemory

		case "PMCP":
			var process globals.Process
			var found bool = false

			globals.SuspReadyQueueMutex.Lock()
			if len(globals.SuspReadyQueue) != 0 {

				sort.Slice(globals.SuspReadyQueue, func(i, j int) bool {
					return globals.SuspReadyQueue[i].Size < globals.SuspReadyQueue[j].Size
				})

				process = globals.SuspReadyQueue[0]
				globals.SuspReadyQueue = globals.SuspReadyQueue[1:]
				found = true
			}
			globals.SuspReadyQueueMutex.Unlock()

			globals.NewQueueMutex.Lock()
			if !found && len(globals.NewQueue) != 0 {

				sort.Slice(globals.NewQueue, func(i, j int) bool {
					return globals.NewQueue[i].Size < globals.NewQueue[j].Size
				})

				process = globals.NewQueue[0]
				globals.NewQueue = globals.NewQueue[1:]
				found = true
			}
			globals.NewQueueMutex.Lock()

			if !found {
				<-globals.LTSEmpty
				continue
			}

			go func(p *globals.Process) {
				InitProcess(p)
				logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d se inicializo en Memoria", p.PCB.GetPID()))
				globals.WaitingForMemory <- struct{}{}
			}(&process)

			<-globals.WaitingForMemory

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
	if len(globals.ReadyQueue) == 1 {
		globals.STSEmpty <- struct{}{}
	}
	globals.ReadyQueueMutex.Unlock()

	logger.RequiredLog(true, process.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})
}

func STS() {
	for {
		switch config.Values.SchedulerAlgorithm {
		case "FIFO":
			var cpu *globals.CPUConnection

			if len(kernel_api.GetCPUList(false)) == 0 {
				<-globals.AvailableCpu
				continue
			} else {
				cpus := kernel_api.GetCPUList(false)
				cpu = &cpus[0]
			}

			var process globals.Process
			var found bool = false // ?

			globals.ReadyQueueMutex.Lock()
			if len(globals.ReadyQueue) != 0 {
				process = globals.ReadyQueue[0]
				globals.ReadyQueue = globals.ReadyQueue[1:]
				found = true
			}
			globals.ReadyQueueMutex.Unlock()

			if !found {
				<-globals.STSEmpty
				continue
			}

			go func(p *globals.Process, c *globals.CPUConnection) {
				SendToExecute(p, c)
				globals.WaitingForCPU <- struct{}{}
			}(&process, cpu)

			<-globals.WaitingForCPU
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

func SendToExecute(process *globals.Process, cpu *globals.CPUConnection) {

	lastState := process.PCB.GetState()
	process.PCB.SetState(pcb.EXEC)
	actualState := process.PCB.GetState()
	logger.RequiredLog(true, process.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})

	cpu.Working = true

	// ? Saco ExecQueue por esta mientras
	globals.ProcessesInExec = append(globals.ProcessesInExec, globals.CurrentProcess{
		Cpu:     *cpu,
		Process: *process,
	})

	request := globals.CPURequest{
		PID: process.PCB.GetPID(),
		PC:  process.PCB.GetPC(),
	}

	dispatchResp, err := kernel_api.SendToWork(*cpu, request)

	if err != nil {
		logger.Instance.Error("Error al enviar el proceso a la CPU", "error", err)
		return
	}

	switch dispatchResp.Motivo {
	case "Interrupt":
		// Temp
		logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d fue interrumpido por la CPU", dispatchResp.PID))
		QueueToReady(process)
		return
	case "Exit":
		logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d fue finalizado por la CPU", dispatchResp.PID))
		return
	default:
		logger.Instance.Error("Motivo desconocido", "Motivo", dispatchResp.Motivo)
		return
	}

}
