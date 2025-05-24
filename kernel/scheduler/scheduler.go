package scheduler

import (
	"fmt"
	"os"
	"sort"
	kernel_api "ssoo-kernel/api"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	processes "ssoo-kernel/processes"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
	slices "ssoo-utils/slices"
)

func LTS() {
	for {
		switch config.Values.ReadyIngressAlgorithm {
		case "FIFO":

			globals.LTSMutex.Lock()
			if slices.IsEmpty(globals.LTS) {
				globals.LTSMutex.Unlock()
				logger.Instance.Info("La cola de procesos en NEW esta vacia")
				<-globals.LTSEmpty
				globals.LTSMutex.Lock()
			}

			process := globals.LTS[0]
			globals.LTS = globals.LTS[1:]
			globals.LTSMutex.Unlock()

			go func(p *globals.Process) {
				InitProcess(p)
				logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d se inicializo en Memoria", p.PCB.GetPID()))
				globals.WaitingForMemory <- struct{}{}
			}(&process)

			<-globals.WaitingForMemory

		case "PMCP":

			globals.LTSMutex.Lock()
			if slices.IsEmpty(globals.LTS) {
				globals.LTSMutex.Unlock()
				logger.Instance.Info("La cola de procesos en NEW esta vacia")
				<-globals.LTSEmpty
				globals.LTSMutex.Lock()
			}

			// Ordenar por tamaño ascendente (más chico primero)
			sort.Slice(globals.LTS, func(i, j int) bool {
				return globals.LTS[i].Size < globals.LTS[j].Size
			})

			process := globals.LTS[0]
			globals.LTS = globals.LTS[1:]
			globals.LTSMutex.Unlock()

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
		logger.Instance.Info(fmt.Sprintf("Se intenta inicializar el proceso con el pid %d en Memoria", process.PCB.GetPID()))
		err := processes.InitializeProcessInMemory(process.PCB.GetPID(), process.GetPath(), process.GetSize())

		if err == nil {
			queueToSTS(process)
			return
		}
		logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d entra en espera. Memoria no pudo inicializarlo", process.PCB.GetPID()))
		<-globals.RetryProcessCh // Este espera ser desbloqueado desde Finalización de Proceso
	}
}

func queueToSTS(process *globals.Process) {
	globals.STSMutex.Lock()
	defer globals.STSMutex.Unlock()
	lastState := process.PCB.GetState()
	process.PCB.SetState(pcb.READY)
	actualState := process.PCB.GetState()
	globals.STS = append(globals.STS, *process)
	//TODO:Lo dejo comentado hasta tener Exit o Interrupt implementado
	if len(globals.STS) == 1 {
		globals.STSEmpty <- struct{}{}
	}

	logger.RequiredLog(true, process.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})
}

func STS() { // Esto hay que tirarlo con una go routine antes de LTS
	for {
		switch config.Values.SchedulerAlgorithm {
		case "FIFO":
			globals.STSMutex.Lock()
			if slices.IsEmpty(globals.STS) {
				globals.STSMutex.Unlock()
				<-globals.STSEmpty
				globals.STSMutex.Lock()
			}

			process := globals.STS[0]
			globals.STS = globals.STS[1:]
			globals.STSMutex.Unlock()

			go func(p *globals.Process) {
				SendToExecute(p)
				globals.WaitingForCPU <- struct{}{}
			}(&process)

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

func SendToExecute(process *globals.Process) {

	lastState := process.PCB.GetState()
	process.PCB.SetState(pcb.EXEC)
	actualState := process.PCB.GetState()
	logger.RequiredLog(true, process.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})

	cpus := kernel_api.GetCPUList(false)

	if slices.IsEmpty(cpus) {
		for {
			cpus = kernel_api.GetCPUList(false)

			if len(cpus) != 0 {
				break
			}
			<-globals.AvailableCpu // Aca deberia buscar donde guardo las nuevas CPUs, y mandar la señal.
		}
	}

	cpu := cpus[0]

	globals.ProcessExec = append(globals.ProcessExec, globals.CurrentProcess{
		Cpu:     cpu,
		Process: *process,
	})

	request := globals.CPURequest{
		PID: process.PCB.GetPID(),
		PC:  process.PCB.GetPC(),
	}

	dispatchResp, err := kernel_api.SendToWork(cpu, request)

	if err != nil {
		logger.Instance.Error("Error al enviar el proceso a la CPU", "error", err)
		return
	}

	switch dispatchResp.Motivo {
	case "Interrupt":
		logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d fue interrumpido por la CPU", dispatchResp.PID))
		return
	case "Exit":
		logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d fue finalizado por la CPU", dispatchResp.PID))
		return
	default:
		logger.Instance.Error("Motivo desconocido", "Motivo", dispatchResp.Motivo)
		return
	}

}
