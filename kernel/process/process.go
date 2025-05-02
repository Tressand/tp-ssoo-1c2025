package process

import (
	"fmt"
	"log/slog"
	"net/http"
	"ssoo-kernel/config"
	kernel_globals "ssoo-kernel/globals"
	"ssoo-utils/httputils"
	"ssoo-utils/pcb"
)

func CreateProcess(pathFile string, processSize int) {
	newProcess := pcb.Create(
		kernel_globals.GetNextPID(),
		pathFile,
	)

	// pido memoria a memoria(?)🥵
	err := requestMemoryProcess(newProcess.GetPID(), pathFile, processSize)
	if err != nil {
		slog.Error("No se pudo crear el proceso en Memoria", "error", err)
		return
	}

	kernel_globals.ActualProcess = newProcess
	kernel_globals.Processes = append(kernel_globals.Processes, newProcess)

	slog.Info(fmt.Sprintf(
		"## (%d) Se crea el proceso - Estado: NEW - Tamaño: %d KB",
		newProcess.GetPID(),
		processSize,
	))
}

/*
var newQueue []*pcb.PCB

	func addToNewQueue(pcb *pcb.PCB) {
		switch config.Values.ReadyIngressAlgorithm {
		case "FIFO":
			newQueue = append(newQueue, pcb)
		case "PMCP":
			// Proceso más chico primero: Ordenamos la cola por tamaño ascendente
			newQueue = append(newQueue, pcb)
			sort.Slice(newQueue, func(i, j int) bool {
				return newQueue[i].Size < newQueue[j].Size
			})

		}
	}
*/
func requestMemoryProcess(pid uint, codePath string, size int) error {

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "/process",
		Queries: map[string]string{
			"pid":  fmt.Sprint(pid),
			"path": codePath,
			"size": fmt.Sprint(size),
		},
	})

	resp, err := http.Post(url, "text/plain", nil)
	if err != nil {
		return fmt.Errorf("error al llamar a Memoria: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Memoria rechazó la creación (código %d)", resp.StatusCode)
	}

	return nil
}
