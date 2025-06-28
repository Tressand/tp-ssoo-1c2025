package scheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	queue "ssoo-kernel/queues"
	process_shared "ssoo-kernel/shared"
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

	for {
		switch config.Values.ReadyIngressAlgorithm {
		case "FIFO":
			slog.Info("Planificando con FIFO")

			var process *globals.Process
			var found bool = false

			globals.SuspReadyQueueMutex.Lock()
			if len(globals.SuspReadyQueue) != 0 {
				process = globals.SuspReadyQueue[0]
				globals.SuspReadyQueue = globals.SuspReadyQueue[1:]
				found = true
				slog.Info("Se encontró un proceso en Susp Ready")
			}
			globals.SuspReadyQueueMutex.Unlock()

			globals.NewQueueMutex.Lock()
			if !found && len(globals.NewQueue) != 0 {
				process = globals.NewQueue[0]
				globals.NewQueue = globals.NewQueue[1:]
				found = true
				slog.Info("Se encontró un proceso en New")
			}
			globals.NewQueueMutex.Unlock()

			if !found {
				slog.Info("No hay procesos en SuspReadyQueue o NewQueue. Se bloquea hasta que se agregen procesos nuevos", "found", found)
				<-globals.LTSEmpty
				continue
			}

			logger.Instance.Debug("Se intenta inicializar un proceso en LTS", "pid", process.PCB.GetPID())

			go process_shared.InititializeProcess(process)
			logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d se inicializo en Memoria", process.PCB.GetPID()))

		case "PMCP":
			logger.Instance.Info("Planificando con PMCP")

			var process *globals.Process
			var found bool = false

			globals.SuspReadyQueueMutex.Lock()
			if len(globals.SuspReadyQueue) != 0 {
				sort.Slice(globals.SuspReadyQueue, func(i, j int) bool {
					return globals.SuspReadyQueue[i].Size < globals.SuspReadyQueue[j].Size
				})

				process = globals.SuspReadyQueue[0]
				globals.SuspReadyQueue = globals.SuspReadyQueue[1:]
				found = true
				logger.Instance.Info("Se encontró un proceso en Susp Ready")
			}
			globals.SuspReadyQueueMutex.Unlock()

			globals.NewQueueMutex.Lock()
			if !found && len(globals.NewQueue) != 0 {
				sort.Slice(globals.NewQueue, func(i, j int) bool {
					return globals.NewQueue[i].Size < globals.NewQueue[j].Size
				})

				process = globals.NewQueue[0]
				globals.NewQueue = globals.NewQueue[1:]
				found = true
				logger.Instance.Info("Se encontró un proceso en New")
			}
			globals.NewQueueMutex.Unlock()

			if !found {
				logger.Instance.Info("No hay procesos en SuspReadyQueue o NewQueue. Se bloquea hasta que se agregen procesos nuevos", "found", found)
				<-globals.LTSEmpty
				continue
			}

			go process_shared.InititializeProcess(process)

		default:
			fmt.Fprintf(os.Stderr, "Algorithm not supported - %s\n", config.Values.ReadyIngressAlgorithm)
			return
		}
	}
}

func STS() {
	for {
		switch config.Values.SchedulerAlgorithm {
		case "FIFO":
			FIFO()
		case "SJF":
			SJF()
		case "SRT":
			SRT()
		default:
			fmt.Fprintf(os.Stderr, "Algorithm not supported - %s\n", config.Values.SchedulerAlgorithm)
			return
		}
	}
}

func FIFO() {
	var cpu *globals.CPUConnection

	globals.AvCPUmu.Lock()
	avCPUs := process_shared.GetCPUList(false)
	found := len(avCPUs) != 0
	globals.AvCPUmu.Unlock()

	if !found {
		slog.Info("No hay CPUs disponibles, esperando a que se libere una")
		<-globals.AvailableCpu
		slog.Info("Se desbloquea STS porque hay CPUs disponibles")
		return
	}

	cpu = avCPUs[0]

	process, err := queue.Dequeue(pcb.READY)

	if err != nil {
		slog.Info("Se bloquea STS porque no hay procesos en READY")
		<-globals.STSEmpty
		slog.Info("Se desbloquea STS porque hay nuevos procesos en READY")
		return
	}

	globals.AvCPUmu.Lock()
	cpu.Working = true // ?
	globals.AvCPUmu.Unlock()

	slog.Info("STS envia a ejecutar el proceso", "pid", process.PCB.GetPID(), " en CPU ", cpu.ID)

	go sendToExecute(process, cpu)
}

func SJF() {
	var cpu *globals.CPUConnection

	globals.AvCPUmu.Lock()
	cpus := process_shared.GetCPUList(false)
	globals.AvCPUmu.Unlock()

	if len(cpus) == 0 {
		<-globals.AvailableCpu
		return
	}

	globals.AvCPUmu.Lock()
	cpu = process_shared.GetCPUList(false)[0]
	globals.AvCPUmu.Unlock()

	var process *globals.Process
	var found bool = false

	slog.Debug("No me bloquie en las cpus")

	globals.ReadyQueueMutex.Lock()
	if len(globals.ReadyQueue) != 0 {
		minIndex := 0
		for i := 1; i < len(globals.ReadyQueue); i++ {
			if globals.ReadyQueue[i].EstimatedBurst < globals.ReadyQueue[minIndex].EstimatedBurst {
				minIndex = i
			}
		}
		process = globals.ReadyQueue[minIndex]
		found = true
		globals.ReadyQueue = append(globals.ReadyQueue[:minIndex], globals.ReadyQueue[minIndex+1:]...)
	}
	globals.ReadyQueueMutex.Unlock()

	slog.Debug("Me bloqueo antes del !found????")

	if !found {
		slog.Info("Me estoy bloqueando en STS porque no hay procesos en READY")
		<-globals.STSEmpty
		slog.Info("Me desbloqueo en STS porque hay procesos en READY")
		return
	}

	slog.Debug("Intento enviar a ejecutar un proceso....")

	go sendToExecute(process, cpu)
}

func SRT() {
	slog.Debug("Entre a SRT")

	globals.AvCPUmu.Lock()
	cpus := process_shared.GetCPUList(false)
	globals.AvCPUmu.Unlock()

	slog.Debug("CPUs disponibles", "cpus", cpus)

	if len(cpus) != 0 {
		slog.Debug("Estoy en SRT, entrando a SJF...")
		SJF()
		return
	}

	slog.Debug("Utilizando la logica especifica de SRT")

	var process *globals.Process
	var found bool = false

	globals.ReadyQueueMutex.Lock()
	if len(globals.ReadyQueue) != 0 {
		minIndex := 0
		for i := 1; i < len(globals.ReadyQueue); i++ {
			if globals.ReadyQueue[i].EstimatedBurst < globals.ReadyQueue[minIndex].EstimatedBurst {
				minIndex = i
			}
		}
		process = globals.ReadyQueue[minIndex]
		found = true
		globals.ReadyQueue = append(globals.ReadyQueue[:minIndex], globals.ReadyQueue[minIndex+1:]...)
	}
	globals.ReadyQueueMutex.Unlock()

	slog.Debug("Me bloquie antes del !found")

	if !found {
		slog.Info("Me estoy bloqueando en STS porque no hay procesos en READY")
		<-globals.STSEmpty
		slog.Info("Me desbloqueo en STS porque hay procesos en READY")
		return
	}

	var forInterrupt bool = false
	var toInterrupt *globals.CPUSlot

	/*
		globals.ExecQueueMutex.Lock()

		if len(globals.ExecQueue) != 0 {
			minBurst := globals.ExecQueue[0].Process.EstimatedBurst

			for i := range globals.ExecQueue {
				slot := &globals.ExecQueue[i]
				if slot.Process.EstimatedBurst > process.EstimatedBurst && slot.Process.EstimatedBurst < minBurst {
					minBurst = slot.Process.EstimatedBurst
					toInterrupt = slot
					forInterrupt = true
				}
			}
		}

		globals.ExecQueueMutex.Unlock()
	*/

	if forInterrupt {
		slog.Debug("No entre a !!")
		err := interruptCPU(*toInterrupt.Cpu, int(toInterrupt.Process.PCB.GetPID()))

		if err != nil {
			slog.Error("Error al interrumpir proceso", "pid", toInterrupt.Process.PCB.GetPID(), "error", err)

			globals.ReadyQueueMutex.Lock()
			globals.ReadyQueue = append(globals.ReadyQueue, process)
			globals.ReadyQueueMutex.Unlock()

			return
		}

		slog.Info("Desalojo ejecutado correctamente", "pid", toInterrupt.Process.PCB.GetPID())

		go sendToExecute(process, toInterrupt.Cpu)
		return
	}

	queue.Enqueue(pcb.READY, process)

	<-globals.WaitingNewProcessInReady
}

func UpdateBurstEstimation(process *globals.Process) {

	realBurst := time.Since(process.StartTime).Seconds()
	previousEstimate := process.EstimatedBurst

	newEstimate := config.Values.Alpha*realBurst + (1-config.Values.Alpha)*previousEstimate

	process.LastRealBurst = realBurst
	process.EstimatedBurst = newEstimate

	slog.Info(fmt.Sprintf("PID %d - Burst real: %.2fs - Estimada previa: %.2f - Nueva estimación: %.2f",
		process.PCB.GetPID(), realBurst, previousEstimate, newEstimate))
}

func interruptCPU(cpu globals.CPUConnection, pid int) error {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       cpu.IP,
		Port:     cpu.Port,
		Endpoint: "interrupt",
	})

	// El PID va como texto plano
	body := bytes.NewReader([]byte(strconv.Itoa(pid)))

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

	queue.Enqueue(pcb.EXEC, process)

	slot := new(globals.CPUSlot)
	slot.Cpu = cpu
	slot.Process = process

	globals.CPUsSlotsMu.Lock()
	globals.CPUsSlots = append(globals.CPUsSlots, slot)
	globals.CPUsSlotsMu.Unlock()

	request := globals.CPURequest{
		PID: process.PCB.GetPID(),
		PC:  process.PCB.GetPC(),
	}

	resp, err := sendToWork(*cpu, request)

	if err != nil {
		globals.AvCPUmu.Lock()
		cpu.Working = false
		globals.AvCPUmu.Unlock()
		slog.Error("Error al enviar el proceso a la CPU", "error", err)
		return
	}

	switch resp.Motivo {
	case "Interrupt":
		slog.Info("El proceso con el pid %d fue interrumpido por la CPU %d", resp.PID, cpu.ID)

		UpdateBurstEstimation(process)

		queue.Enqueue(pcb.READY, process)

		globals.AvCPUmu.Lock()
		cpu.Working = false
		globals.AvCPUmu.Unlock()

		return
	case "Exit":
		UpdateBurstEstimation(process)
		logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d fue finalizado por la CPU", resp.PID))

		select {
		case globals.WaitingNewProcessInReady <- struct{}{}:
			slog.Debug("Replanificando STS")
		default:
		}

		return
	default:
		logger.Instance.Error("Motivo desconocido", "Motivo", resp.Motivo)
		return
	}

}

func changeAvailableCpu(cpu *globals.CPUConnection, working bool) {
	globals.AvCPUmu.Lock()
	defer globals.AvCPUmu.Unlock()
	for i := range globals.AvailableCPUs {
		if globals.AvailableCPUs[i].ID == cpu.ID {
			globals.AvailableCPUs[i].Working = working
			break
		}
	}
}

func sendToWork(cpu globals.CPUConnection, request globals.CPURequest) (globals.DispatchResponse, error) {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       cpu.IP,
		Port:     cpu.Port,
		Endpoint: "dispatch",
	})

	jsonRequest, err := json.Marshal(request)
	if err != nil {
		logger.Instance.Error("Error marshaling request to JSON", "error", err)
		return globals.DispatchResponse{}, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(jsonRequest))
	if err != nil {
		logger.Instance.Error("Error making POST request", "error", err)
		return globals.DispatchResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTeapot {
			logger.Instance.Warn("Server requested shutdown")
			return globals.DispatchResponse{}, fmt.Errorf("server asked for shutdown")
		}
		logger.Instance.Error("Unexpected status code from CPU", "status", resp.StatusCode)
		return globals.DispatchResponse{}, fmt.Errorf("unexpected response status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Instance.Error("Error reading response body", "error", err)
		return globals.DispatchResponse{}, err
	}

	var dispatchResp globals.DispatchResponse
	err = json.Unmarshal(data, &dispatchResp)
	if err != nil {
		logger.Instance.Error("Error unmarshaling response", "error", err)
		return globals.DispatchResponse{}, err
	}

	return dispatchResp, nil
}
