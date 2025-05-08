package process

import (
	"fmt"
	"net/http"
	"os"
	"ssoo-cpu/config"
	globals "ssoo-kernel/globals"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
)

func CreateProcess(path string, size int) {
	newProcess := globals.Process{
		PCB: pcb.Create(
			GetNextPID(),
			path,
		),
		Path: path,
		Size: size,
	}

	QueueToLTS(newProcess)

	logger.RequiredLog(true, newProcess.PCB.GetPID(), "Se crea el proceso", map[string]string{"Estado": newProcess.PCB.GetState().String(), "Tamaño": fmt.Sprintf("%d KB", size)})
}

func QueueToLTS(process globals.Process) {
	globals.LTSMutex.Lock()
	defer globals.LTSMutex.Unlock()
	globals.LTS = append(globals.LTS, process)
}

func GetNextPID() uint {
	globals.PIDMutex.Lock()
	defer globals.PIDMutex.Unlock()
	pid := globals.NextPID
	globals.NextPID++
	return pid
}

func InitializeProcessInMemory(pid uint, codePath string, size int) error {
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
