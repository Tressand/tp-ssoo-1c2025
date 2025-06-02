package processes

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"ssoo-kernel/config"
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

	QueueToNew(newProcess)

	logger.RequiredLog(true, newProcess.PCB.GetPID(), "Se crea el proceso", map[string]string{"Estado": newProcess.PCB.GetState().String(), "Tamaño": fmt.Sprintf("%d KB", size)})
}

func SearchProcessInExec(id string) (*globals.Process, error) {
	for _, processExec := range globals.ProcessesInExec {
		if processExec.Cpu.ID == id {
			fmt.Printf("Proceso de pid %v encontrado", processExec.Process.PCB.GetPID())
			return &processExec.Process, nil
		}
	}

	return nil, errors.New("proceso no encontrado")
}

func TerminateProcess(process *globals.Process) {
	pid := process.PCB.GetPID()
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "process",
		Queries: map[string]string{
			"pid": fmt.Sprint(pid),
		},
	})

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		logger.RequiredLog(true, pid, "Error creando request DELETE", map[string]string{"Error": err.Error()})
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.RequiredLog(true, pid, "Error al eliminar el proceso de memoria", map[string]string{"Error": err.Error()})
		return
	}

	if resp.StatusCode != http.StatusOK {
		logger.RequiredLog(true, pid, "Error al eliminar el proceso de memoria", map[string]string{"Código": fmt.Sprint(resp.StatusCode)})
		return
	}

	defer resp.Body.Close()

	logger.RequiredLog(true, pid, "", map[string]string{"Métricas de estado:": process.PCB.GetKernelMetrics().String()})

	if len(globals.SuspReadyQueue) == 0 && globals.ProcessWaiting {
		globals.RetryProcessCh <- struct{}{}
		fmt.Println("No hay procesos en SUSP. READY. Se intenta inicializar el proceso en memoria")
	} else {
		fmt.Println("Hay procesos en READY SUSPEND")
	}

}

func QueueToNew(process globals.Process) {
	globals.NewQueueMutex.Lock()
	defer globals.NewQueueMutex.Unlock()
	globals.NewQueue = append(globals.NewQueue, process)

	if len(globals.NewQueue) == 1 && globals.SchedulerStatus == "START" { // Estaba vacia
		globals.LTSEmpty <- struct{}{} // ? Revisar sí sirve tomando en cuenta que ahora hay 2 colas
	}

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
