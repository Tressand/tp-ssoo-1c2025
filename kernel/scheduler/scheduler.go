package scheduler

import (
	"fmt"
	"os"
	"sort"
	kernel_cpu_api "ssoo-kernel/api"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	process_module "ssoo-kernel/process"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
	slices "ssoo-utils/slices"
)

var RetryProcessCh = make(chan struct{}) // Esto deberia ser activado luego en Finalización de procesos
var WaitingForMemory = make(chan struct{}, 1)

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
				WaitingForMemory <- struct{}{}
			}(&process)

			<-WaitingForMemory

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
				WaitingForMemory <- struct{}{}
			}(&process)

			<-WaitingForMemory

		default:
			fmt.Fprintf(os.Stderr, "Algorithm not supported - %s\n", config.Values.ReadyIngressAlgorithm)
			return
		}
	}
}

func InitProcess(process *globals.Process) {
	for {
		logger.Instance.Info(fmt.Sprintf("Se intenta inicializar el proceso con el pid %d en Memoria", process.PCB.GetPID()))
		err := process_module.InitializeProcessInMemory(process.PCB.GetPID(), process.GetPath(), process.GetSize())

		if err == nil {
			queueToSTS(process)
			return
		}
		logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d entra en espera. Memoria no pudo inicializarlo", process.PCB.GetPID()))
		<-RetryProcessCh // Este espera ser desbloqueado desde Finalización de Proceso
	}
}

func queueToSTS(process *globals.Process) {
	globals.STSMutex.Lock()
	defer globals.STSMutex.Unlock()
	lastState := process.PCB.GetState()
	process.PCB.SetState(pcb.READY)
	actualState := process.PCB.GetState()
	globals.STS = append(globals.STS, *process)
	/* TODO: Me estaba bloqueando el planificador jajajja, sacar cuando este andando STS
	if len(globals.STS) == 1 {
		globals.STSEmpty <- struct{}{}
	}
	*/
	logger.RequiredLog(true, process.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})
}

var WaitingForCPU = make(chan struct{}, 1)

func STS() {
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
				WaitingForCPU <- struct{}{}
			}(&process)

			<-WaitingForCPU
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

var AvailableCpu = make(chan struct{}, 1)

func SendToExecute(process *globals.Process) {
	globals.STSMutex.Lock()

	lastState := process.PCB.GetState()
	process.PCB.SetState(pcb.EXEC)
	actualState := process.PCB.GetState()
	logger.RequiredLog(true, process.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})

	globals.STSMutex.Unlock()

	cpus := kernel_cpu_api.GetCPUList(false)

	if slices.IsEmpty(cpus) {
		for {
			cpus = kernel_cpu_api.GetCPUList(false)

			if len(cpus) != 0 {
				break
			}
			<-AvailableCpu
		}
	}

	cpu := cpus[0]

	cpu.SendToWork(kernel_cpu_api.CPURequest{
		PID: process.PCB.GetPID(),
		PC:  process.PCB.GetPC(),
	})
}
