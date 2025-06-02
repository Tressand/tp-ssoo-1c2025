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
			cpu = &kernel_api.GetCPUList(false)[0]
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
				SendToExecute(p, c)
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

func SendToExecute(process *globals.Process, cpu *globals.CPUConnection) {

	lastState := process.PCB.GetState()
	process.PCB.SetState(pcb.EXEC)
	actualState := process.PCB.GetState()
	logger.RequiredLog(true, process.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})

	cpu.Working = true

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
