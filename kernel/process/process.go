package process

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"ssoo-kernel/config"
	kernel_globals "ssoo-kernel/globals"
	"ssoo-utils/httputils"
	"ssoo-utils/pcb"
)

// Estructura para manejar la cola de nuevos procesos
type ProcessQueueItem struct {
	PCB  *pcb.PCB
	Size int
}

var NewQueue []ProcessQueueItem = make([]ProcessQueueItem, 0)

func CreateProcess(pathFile string, processSize int) {
	newProcess := pcb.Create(
		kernel_globals.GetNextPID(),
		pathFile,
	)

	err := requestMemoryProcess(newProcess.GetPID(), pathFile, processSize)
	if err != nil {
		slog.Error("No se pudo crear el proceso en Memoria", "error", err)
		return
	}

	addToNewQueue(newProcess, processSize)

	kernel_globals.Processes = append(kernel_globals.Processes, newProcess)

	slog.Info(fmt.Sprintf(
		"## (%d) Se crea el proceso - Estado: NEW - Tamaño: %d KB",
		newProcess.GetPID(),
		processSize,
	))
}

func addToNewQueue(pcb *pcb.PCB, size int) {
	newItem := ProcessQueueItem{
		PCB:  pcb,
		Size: size,
	}

	switch config.Values.ReadyIngressAlgorithm {
	case "FIFO":
		NewQueue = append(NewQueue, newItem)
	case "PMCP":
		// Proceso más chico primero: Ordenamos la cola por tamaño ascendente
		NewQueue = append(NewQueue, newItem)
		sort.Slice(NewQueue, func(i, j int) bool {
			return NewQueue[i].Size < NewQueue[j].Size
		})
	default:
		NewQueue = append(NewQueue, newItem) // Por defecto FIFO
	}
}

// Función para obtener el siguiente proceso de la cola
func GetNextProcessFromQueue() (*pcb.PCB, int) {
	if len(NewQueue) == 0 {
		return nil, 0
	}
	nextItem := NewQueue[0]
	NewQueue = NewQueue[1:] // Elimina el primer elemento
	return nextItem.PCB, nextItem.Size
}

func requestMemoryProcess(pid uint, codePath string, size int) error {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "process",
		Queries: map[string]string{
			"pid": fmt.Sprint(pid),
			"req": fmt.Sprint(size),
		},
	})

	codeFile, err := os.OpenFile(codePath, os.O_RDONLY, 0666)
	if err != nil {
		return fmt.Errorf("error al abrir el archivo de código: %v", err)
	}

	resp, err := http.Post(url, "text/plain", codeFile)
	if err != nil {
		return fmt.Errorf("error al llamar a Memoria: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("memoria rechazó la creación (código %d)", resp.StatusCode)
	}

	return nil
}
