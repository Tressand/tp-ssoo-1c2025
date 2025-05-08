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
var WaitingForMemoryCh = make(chan struct{})

func LTS() {
	for {
		switch config.Values.ReadyIngressAlgorithm {
		case "FIFO":
			<-RetryProcessCh

			globals.LTSMutex.Lock()
			process := globals.LTS[0]
			globals.LTS = globals.LTS[1:]

			fmt.Println(globals.LTS)
			fmt.Println(process)
			globals.LTSMutex.Unlock()

			<-WaitingForMemoryCh

			go InitProcessFIFO(&process)

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
			RetryProcessCh <- struct{}{}
			WaitingForMemoryCh <- struct{}{}
			return
		}
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
