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

	var sortBy queues.SortBy

	switch config.Values.ReadyIngressAlgorithm {
	case "FIFO":
		sortBy = queues.NoSort
	case "PMCP":
		sortBy = queues.Size
	default:
		fmt.Fprintf(os.Stderr, "Algorithm not supported - %s\n", config.Values.ReadyIngressAlgorithm)
		return
	}

	for {
		if globals.WaitingForRetry {
			<-globals.BlockedForMemory
			continue
		}

		var process *globals.Process
		var queue pcb.STATE = pcb.SUSP_READY

		// Sí vamos a necesitar que Kernel se apague cuando todas las colas esten vacias.
		// El Dequeue deberia solo darme la referencia al proceso y no quitarlo de la cola.

		process = queues.Dequeue(queue, sortBy)

		if process == nil {
			queue = pcb.NEW
			process = queues.Dequeue(queue, sortBy)
		}
		if process == nil {
			slog.Info("No hay procesos pendientes. Se bloquea hasta que se agregen procesos nuevos")
			<-globals.LTSEmpty
			continue
		}

		slog.Debug("Se encontró un proceso pendiente, inicializando...", "pid", process.PCB.GetPID(), "queue", queue)

		shared.InititializeProcess(process)
	}
}

func STS() {
	slog.Info("STS iniciado")

	if shared.ListCPUsIsEmpty(globals.Any) {
		slog.Debug("No hay CPUs conectadas, esperando a que se conecte una")
		<-globals.CpuAvailableSignal
	}

	var sortBy queues.SortBy

	switch config.Values.SchedulerAlgorithm {
	case "FIFO":
		sortBy = queues.NoSort
	case "SJF", "SRT":
		sortBy = queues.EstimatedBurst
	default:
		panic("algoritmo de planificación de corto plazo inválido, se ordena matar al culpable.")
	}

	for {
		var cpu *globals.CPUConnection = shared.GetCPU(globals.Available)
		if cpu != nil {
			process := queues.Dequeue(pcb.READY, sortBy)

			if process == nil {
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
			process := queues.Search(pcb.READY, sortBy)

			if process == nil {
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
					queues.Enqueue(pcb.READY, process)
					return
				}

				process = queues.RemoveByPID(pcb.READY, process.PCB.GetPID())

				if process == nil {
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
	return config.Values.SchedulerAlgorithm == "SRT" && len(globals.CPUsSlots) != 0
}

func GetSlotWithShortestBurst() *globals.CPUSlot {
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

	resp, err := http.Post(url, "text/plain", bytes.NewReader([]byte(fmt.Sprint(pid))))
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
	queues.Enqueue(pcb.EXEC, process)

	exists := false
	for _, slot := range globals.CPUsSlots {
		if slot.Cpu.ID == cpu.ID {
			exists = true
			globals.CPUsSlotsMu.Lock()
			slot.Process = process
			slot.Cpu.State = globals.Occupied
			globals.CPUsSlotsMu.Unlock()
			slog.Debug("Se actualiza el slot de la CPU con el proceso", "cpuID", cpu.ID, "pid", process.PCB.GetPID())
			break
		}
	}

	if !exists {
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

	if err == nil {
		slog.Info("Proceso enviado a la CPU correctamente", "pid", process.PCB.GetPID(), "cpuID", cpu.ID)
		return
	}

	slog.Error(err.Error())

	process = queues.RemoveByPID(pcb.EXEC, process.PCB.GetPID())
	if process == nil {
		return
	}

	queues.Enqueue(pcb.READY, process)

	globals.AvCPUmu.Lock()
	cpu.State = globals.Available
	globals.AvCPUmu.Unlock()

	for _, slot := range globals.CPUsSlots {
		if slot.Cpu.ID == cpu.ID {
			globals.CPUsSlotsMu.Lock()
			slot.Process = nil
			globals.CPUsSlotsMu.Unlock()
			break
		}
	}
}

func newCpuSlot(process *globals.Process, cpu *globals.CPUConnection) *globals.CPUSlot {
	slot := new(globals.CPUSlot)
	slot.Process = process
	slot.Cpu = cpu
	slot.Cpu.State = globals.Occupied
	slog.Debug("Se crea un nuevo slot de CPU", "cpuID", cpu.ID, "pid", process.PCB.GetPID())
	return slot
}

func freeSlot(id string) {
	for i, slot := range globals.CPUsSlots {
		if slot.Cpu.ID == id {
			slog.Debug("Se libera el slot de la CPU", "cpuID", id)
			globals.CPUsSlotsMu.Lock()
			globals.CPUsSlots[i].Process = nil
			globals.CPUsSlots[i].Cpu.State = globals.Available
			globals.CPUsSlotsMu.Unlock()
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
	list := make([]*globals.BlockedByIO, 0)
	for _, elem := range globals.MTSQueue {
		if !elem.TimerStarted {
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

	process := removeProcess(blocked)

	queues.Enqueue(pcb.SUSP_BLOCKED, process)

	requestSuspend(process)
}

func removeProcess(waiting *globals.BlockedByIO) *globals.Process {
	return queues.RemoveByPID(waiting.Process.PCB.GetState(), waiting.Process.PCB.GetPID())
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
