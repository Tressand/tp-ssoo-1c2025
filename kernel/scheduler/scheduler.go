package scheduler

import (
	"fmt"
	"os"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	process_module "ssoo-kernel/process"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
)

var RetryProcessCh = make(chan struct{}) // Esto deberia ser activado luego en Finalización de procesos
var WaitingForMemoryCh = make(chan struct{}, 1)

func LTS() {
	for {
		switch config.Values.ReadyIngressAlgorithm {
		case "FIFO":

			globals.LTSMutex.Lock()
			if len(globals.LTS) == 0 {
				globals.LTSMutex.Unlock()
				<-globals.LTSEmpty
				globals.LTSMutex.Lock()
			}

			<-WaitingForMemoryCh
			
			process := globals.LTS[0]
			globals.LTS = globals.LTS[1:]
			globals.LTSMutex.Unlock()

			go func(p *globals.Process) {
				InitProcessFIFO(p)
				WaitingForMemoryCh <- struct{}{}
			}(&process)

		case "PMCP":
			fmt.Println("PMCP")
		default:
			fmt.Fprintf(os.Stderr, "Algorithm not supported - %s\n", config.Values.ReadyIngressAlgorithm)
		}
	}
}

func InitProcessFIFO(process *globals.Process) {
	for {
		err := process_module.InitializeProcessInMemory(process.PCB.GetPID(), process.GetPath(), process.GetSize())
		fmt.Println(err)
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
	logger.RequiredLog(true, process.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})
}
