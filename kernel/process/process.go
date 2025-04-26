package process

import (
	"fmt"
	"log/slog"
	kernel_globals "ssoo-kernel/globals"
	"ssoo-utils/pcb"
)

func CreateProcess(pathFile string, processSize int) {
	newProcess := pcb.Create(
		kernel_globals.GetNextPID(),
		pathFile,
	)

	kernel_globals.ActualProcess = newProcess
	kernel_globals.Processes = append(kernel_globals.Processes, newProcess)

	slog.Info(fmt.Sprintf("## (%d) Se crea el proceso - Estado: NEW", newProcess.GetPID()))
}
