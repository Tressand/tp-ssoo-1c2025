package scheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	"ssoo-kernel/queues"
	queue "ssoo-kernel/queues"
	shared "ssoo-kernel/shared"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
	"strconv"
	"time"
)

func LTS() {
	<-globals.LTSStopped
	globals.SchedulerStatus = "START"
	slog.Info("LTS iniciado")
	slog.Info("Planificando con ", "algoritmo", config.Values.ReadyIngressAlgorithm)

	var sortBy queue.SortBy

	switch config.Values.ReadyIngressAlgorithm {
	case "FIFO":
		sortBy = queue.NoSort
	case "PMCP":
		sortBy = queue.Size
	default:
		fmt.Fprintf(os.Stderr, "Algorithm not supported - %s\n", config.Values.ReadyIngressAlgorithm)
		return
	}

	for {
		if globals.IsAnyProcessPendingInit() {
			<-globals.BlockedForMemory
			continue
		}

		var process *globals.Process
		var err error
		var found bool = false

		// Sí vamos a necesitar que Kernel se apague cuando todas las colas esten vacias.
		// El Dequeue deberia solo darme la referencia al proceso y no quitarlo de la cola.

		process, err = queues.Dequeue(pcb.SUSP_READY, sortBy)

		if err == nil {
			found = true
			slog.Debug("Se encontró un proceso en Susp Ready", "pid", process.PCB.GetPID())
		} else {
			slog.Debug("No se encontró un proceso en Susp Ready")
		}

		if !found {
			process, err = queues.Dequeue(pcb.NEW, sortBy)

			if err == nil {
				found = true
				slog.Debug("Se encontró un proceso en New", "pid", process.PCB.GetPID())
			} else {
				slog.Debug("No se encontró un proceso en New")
			}
		}

		if !found {
			slog.Info("No hay procesos en SuspReadyQueue o NewQueue. Se bloquea hasta que se agregen procesos nuevos")
			<-globals.LTSEmpty
			continue
		}

		slog.Debug("Se intenta inicializar un proceso en LTS", "pid", process.PCB.GetPID())

		shared.InititializeProcess(process)
	}
}

func STS() {
	slog.Info("STS iniciado")

	if shared.ListCPUsIsEmpty(globals.Any) {
		slog.Debug("No hay CPUs conectadas, esperando a que se conecte una")
		<-globals.CpuAvailableSignal
	}

	var sortBy queue.SortBy

	switch config.Values.SchedulerAlgorithm {
	case "FIFO":
		sortBy = queue.NoSort
	case "SJF", "SRT":
		sortBy = queue.EstimatedBurst
	default:
		fmt.Fprintf(os.Stderr, "Algorithm not supported - %s\n", config.Values.ReadyIngressAlgorithm)
		return
	}

	for {
		if !shared.ListCPUsIsEmpty(globals.Available) {
			cpu := shared.GetCPU(globals.Available)

			if cpu == nil {
				slog.Error("No se encontró una CPU disponible")
				continue
			}

			process, readyIsEmpty := queue.Dequeue(pcb.READY, sortBy)

			if readyIsEmpty != nil {
				slog.Info("Se bloquea STS porque no hay procesos en READY")
				<-globals.STSEmpty
				slog.Info("Se desbloquea STS porque hay nuevos procesos en READY")
				continue
			}

			slog.Debug("STS encontró un proceso en READY", "pid", process.PCB.GetPID())

			go sendToExecute(process, cpu)
			continue
		}

		if ShouldTryInterrupt() {
			process, readyIsEmpty := queue.Search(pcb.READY, sortBy)

			if readyIsEmpty != nil {
				slog.Info("Se bloquea STS porque no hay procesos en READY")
				<-globals.STSEmpty
				slog.Info("Se desbloquea STS porque hay nuevos procesos en READY")
				continue
			}

			minSlot := GetSlotWithShortestBurst()

			interrupt := minSlot != nil && minSlot.Process.EstimatedBurst > process.EstimatedBurst

			if interrupt {
				err := interruptCPU(minSlot.Cpu, minSlot.Process.PCB.GetPID())

				if err != nil {
					slog.Error("Error al interrumpir proceso", "pid", minSlot.Process.PCB.GetPID(), "error", err)
					queue.Enqueue(pcb.READY, process)
					return
				}

				process, err = queue.RemoveByPID(pcb.READY, process.PCB.GetPID())

				if err != nil {
					slog.Error("Error al remover el proceso de la cola READY", "pid", process.PCB.GetPID(), "error", err)
					return
				}

				go sendToExecute(process, minSlot.Cpu)
			}

		}

		slog.Debug("No hay CPUs disponibles, esperando a que se libere una")
		<-globals.CpuAvailableSignal
		slog.Debug("Se desbloquea STS porque hay CPUs disponibles")
	}
}

func ShouldTryInterrupt() bool {
	globals.CPUsSlotsMu.Lock()
	defer globals.CPUsSlotsMu.Unlock()
	return config.Values.SchedulerAlgorithm == "SRT" && len(globals.CPUsSlots) != 0
}

func GetSlotWithShortestBurst() *globals.CPUSlot {
	globals.CPUsSlotsMu.Lock()
	defer globals.CPUsSlotsMu.Unlock()
	minSlot := globals.CPUsSlots[0]
	for _, slot := range globals.CPUsSlots {
		if slot.Process != nil && slot.Process.EstimatedBurst < minSlot.Process.EstimatedBurst {
			minSlot = slot
		}
	}
	return minSlot
}

func interruptCPU(cpu *globals.CPUConnection, pid uint) error {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       cpu.IP,
		Port:     cpu.Port,
		Endpoint: "interrupt",
	})

	body := bytes.NewReader([]byte(strconv.Itoa(int(pid))))

	resp, err := http.Post(url, "text/plain", body)
	if err != nil {
		logger.Instance.Error("Error enviando interrupción a CPU", "ip", cpu.IP, "port", cpu.Port, "pid", pid, "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Instance.Error("CPU respondió con error a la interrupción", "status", resp.StatusCode, "ip", cpu.IP, "port", cpu.Port, "pid", pid)
		return fmt.Errorf("interrupción fallida: status code %d", resp.StatusCode)
	}

	logger.Instance.Info("Interrupción enviada correctamente", "ip", cpu.IP, "port", cpu.Port, "pid", pid)
	return nil
}

func sendToExecute(process *globals.Process, cpu *globals.CPUConnection) {

	slog.Debug("Se agrega al proceso a EXEC", "pid", process.PCB.GetPID())

	queue.Enqueue(pcb.EXEC, process)

	slog.Debug("Se asocia el proceso a la CPU", "pid", process.PCB.GetPID(), "cpuID", cpu.ID)

	globals.CPUsSlotsMu.Lock()
	exists := false
	for _, slot := range globals.CPUsSlots {
		if slot.Cpu.ID == cpu.ID {
			exists = true
			slot.Process = process
			slot.Cpu.State = globals.Occupied
			slog.Debug("Se actualiza el slot de la CPU con el proceso", "cpuID", cpu.ID, "pid", process.PCB.GetPID())
			break
		}
	}
	globals.CPUsSlotsMu.Unlock()

	if !exists {
		slog.Debug("No existe un slot para la CPU, se crea uno nuevo", "cpuID", cpu.ID)
		slot := newCpuSlot(process, cpu)
		globals.CPUsSlotsMu.Lock()
		globals.CPUsSlots = append(globals.CPUsSlots, slot)
		globals.CPUsSlotsMu.Unlock()
	}

	request := globals.CPURequest{
		PID: process.PCB.GetPID(),
		PC:  process.PCB.GetPC(),
	}

	slog.Debug("Se envia a ejecutar el proceso", "PID", process.PCB.GetPID(), "CPU", cpu.ID)

	err := sendToWork(*cpu, request)

	slog.Debug("La CPU termino de trabajar" , "PID", process.PCB.GetPID(), "CPU", cpu.ID)

	if err != nil {
		slog.Debug(err.Error())
		process, err := queue.RemoveByPID(pcb.EXEC, process.PCB.GetPID())

		if err != nil {
			slog.Error("Error al remover el proceso de la cola EXEC", "pid", process.PCB.GetPID(), "error", err)
			return
		}
		err = queue.Enqueue(pcb.READY, process)

		if err != nil {
			slog.Error("Error al re-enqueue el proceso en READY", "pid", process.PCB.GetPID(), "error", err)
		}

		globals.AvCPUmu.Lock()
		cpu.State = globals.Available
		globals.AvCPUmu.Unlock()

		globals.CPUsSlotsMu.Lock()
		for _, slot := range globals.CPUsSlots {
			if slot.Cpu.ID == cpu.ID {
				slot.Process = nil
				break
			}
		}
		globals.CPUsSlotsMu.Unlock()

		slog.Error("Error al enviar el proceso a la CPU", "error", err)
		return
	} else {
		slog.Info("Proceso enviado a la CPU correctamente", "pid", process.PCB.GetPID(), "cpuID", cpu.ID)
	}

}

func newCpuSlot(process *globals.Process, cpu *globals.CPUConnection) *globals.CPUSlot {
	globals.CPUsSlotsMu.Lock()
	defer globals.CPUsSlotsMu.Unlock()
	slot := new(globals.CPUSlot)
	slot.Process = process
	slot.Cpu = cpu
	slot.Cpu.State = globals.Occupied
	slog.Debug("Se crea un nuevo slot de CPU", "cpuID", cpu.ID, "pid", process.PCB.GetPID())
	return slot
}

func freeSlot(id string) {
	globals.CPUsSlotsMu.Lock()
	defer globals.CPUsSlotsMu.Unlock()

	for i, slot := range globals.CPUsSlots {
		if slot.Cpu.ID == id {
			slog.Debug("Se libera el slot de la CPU", "cpuID", id)
			globals.CPUsSlots[i].Process = nil
			globals.CPUsSlots[i].Cpu.State = globals.Available
			return
		}
	}

	slog.Warn("No se encontró un slot para liberar en la CPU", "cpuID", id)
}

func sendToWork(cpu globals.CPUConnection, request globals.CPURequest) error {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       cpu.IP,
		Port:     cpu.Port,
		Endpoint: "dispatch",
	})

	jsonRequest, err := json.Marshal(request)
	if err != nil {
		logger.Instance.Error("Error marshaling request to JSON", "error", err)
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(jsonRequest))
	if err != nil {
		logger.Instance.Error("Error making POST request", "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTeapot {
			logger.Instance.Warn("Server requested shutdown")
			return fmt.Errorf("server asked for shutdown")
		}
		logger.Instance.Error("Unexpected status code from CPU", "status", resp.StatusCode)
		return fmt.Errorf("unexpected response status: %d", resp.StatusCode)
	}

	return nil
}

func withoutTimerStarted() []*globals.BlockedByIO {
	globals.MTSQueueMu.Lock()
	defer globals.MTSQueueMu.Unlock()
	list := make([]*globals.BlockedByIO, 0)
	for _, elem := range globals.MTSQueue {
		if elem.TimerStarted == false {
			list = append(list, elem)
		}
	}
	return list
}

func MTS() {
	for {
		forInitTimer := withoutTimerStarted()

		if len(forInitTimer) == 0 {
			<-globals.MTSEmpty
			continue
		}

		for _, blocked := range forInitTimer {
			blocked.TimerStarted = true
			go sendToWait(blocked)
		}
	}
}

func sendToWait(blocked *globals.BlockedByIO) {

	slog.Debug("Se inicia el timer para el proceso bloqueado por IO", "pid", blocked.Process.PCB.GetPID(), "IOName", blocked.IOName)
	time.Sleep(time.Duration(config.Values.SuspensionTime) * time.Millisecond)
	slog.Info("Tiempo de espera para IO agotado. Se mueve de memoria principal a swap", "pid", blocked.Process.PCB.GetPID(), "IOName", blocked.IOName)

	process, err := removeProcess(blocked)
	if err != nil {
		slog.Error("Error al remover el proceso de la cola", "pid", blocked.Process.PCB.GetPID(), "error", err)
		return
	}

	err = queue.Enqueue(pcb.SUSP_BLOCKED, process)
	if err != nil {
		slog.Error("Error al re-enqueue el proceso en SUSP_BLOCKED", "pid", process.PCB.GetPID(), "error", err)
		return
	}

	requestSuspend(process)
}

func removeProcess(waiting *globals.BlockedByIO) (*globals.Process, error) {
	return queue.RemoveByPID(waiting.Process.PCB.GetState(), waiting.Process.PCB.GetPID())
}

func requestSuspend(process *globals.Process) error {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "suspend",
		Queries: map[string]string{
			"pid": strconv.Itoa(int(process.PCB.GetPID())),
		},
	})

	resp, err := http.Post(url, "text/plain", nil)

	if err != nil {
		logger.Instance.Error("Error al enviar solicitud de swap", "pid", process.PCB.GetPID(), "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Instance.Error("Swap rechazó la solicitud", "pid", process.PCB.GetPID(), "status", resp.StatusCode)
		return fmt.Errorf("swap request failed with status code %d", resp.StatusCode)
	}

	select {
	case globals.RetryInitialization <- struct{}{}:
		slog.Debug("Se desbloquea RetryInitialization porque se realizó un swap exitoso")
	default:
	}

	return nil
}
