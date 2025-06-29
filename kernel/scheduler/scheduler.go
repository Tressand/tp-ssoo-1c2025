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
	slog.Info("STS iniciado")

	// ? Dentro o fuera del for???
	globals.AvCPUmu.Lock()
	noCPUsConnected := len(globals.AvailableCPUs) == 0
	globals.AvCPUmu.Unlock()

	if noCPUsConnected {
		slog.Debug("No hay CPUs conectadas, esperando a que se conecte una nueva")
		<-globals.NewCpuConnected
	}

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
	slog.Debug("Entre a FIFO")

	globals.AvCPUmu.Lock()
	availableCPUs := process_shared.GetCPUList(false)
	noCPUsAvailable := len(availableCPUs) == 0
	globals.AvCPUmu.Unlock()

	if noCPUsAvailable {
		slog.Info("No hay CPUs disponibles, esperando a que se libere una")
		<-globals.CpuBecameIdle
		slog.Info("Se desbloquea STS porque hay CPUs disponibles")
		return
	}

	var cpu *globals.CPUConnection

	cpu = availableCPUs[0]

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
	slog.Debug("Entre a SJF")

	globals.AvCPUmu.Lock()
	availableCPUs := process_shared.GetCPUList(false)
	noCPUsAvailable := len(availableCPUs) == 0
	globals.AvCPUmu.Unlock()

	if noCPUsAvailable {
		slog.Info("No hay CPUs disponibles, esperando a que se libere una")
		<-globals.CpuBecameIdle
		slog.Info("Se desbloquea STS porque hay CPUs disponibles")
		return
	}

	var cpu *globals.CPUConnection

	globals.AvCPUmu.Lock() // chatgpt, decis que esto sea util???
	cpu = availableCPUs[0]
	globals.AvCPUmu.Unlock()

	var process *globals.Process

	globals.ReadyQueueMutex.Lock()
	isEmpty := len(globals.ReadyQueue) == 0
	globals.ReadyQueueMutex.Unlock()

	if isEmpty {
		slog.Info("Me estoy bloqueando en STS porque no hay procesos en READY")
		<-globals.STSEmpty
		slog.Info("Me desbloqueo en STS porque hay procesos en READY")
		return
	}

	globals.ReadyQueueMutex.Lock()
	isUnique := len(globals.ReadyQueue) == 1
	globals.ReadyQueueMutex.Unlock()

	globals.ReadyQueueMutex.Lock()
	if isUnique {
		slog.Debug("Hay un solo proceso en la ReadyQueue")
		process = globals.ReadyQueue[0]
		globals.ReadyQueue = globals.ReadyQueue[1:]
	} else {
		slog.Debug("Buscando el proceso con menor EstimatedBurst en la ReadyQueue")
		minIndex := 0
		for i := 1; i < len(globals.ReadyQueue); i++ {
			if globals.ReadyQueue[i].EstimatedBurst < globals.ReadyQueue[minIndex].EstimatedBurst {
				minIndex = i
			}
		}
		process = globals.ReadyQueue[minIndex]
		globals.ReadyQueue = append(globals.ReadyQueue[:minIndex], globals.ReadyQueue[minIndex+1:]...)
	}
	globals.ReadyQueueMutex.Unlock()

	slog.Debug("SJF envia el proceso ", "pid", process.PCB.GetPID(), " a ejecutar en CPU ", cpu.ID)

	go sendToExecute(process, cpu)
}

func SRT() {
	slog.Debug("Entre a SRT")

	globals.AvCPUmu.Lock()
	cpus := process_shared.GetCPUList(false)
	cpusAvailable := len(cpus) != 0
	globals.AvCPUmu.Unlock()

	slog.Debug("CPUs disponibles", "cpus", cpus)

	if cpusAvailable {
		slog.Debug("Se utiliza SJF porque hay CPUs disponibles")
		SJF()
		return
	}

	slog.Debug("Utilizando la logica especifica de SRT")

	var process *globals.Process

	globals.ReadyQueueMutex.Lock()
	isEmpty := len(globals.ReadyQueue) == 0
	globals.ReadyQueueMutex.Unlock()

	if isEmpty {
		slog.Info("Me estoy bloqueando en STS porque no hay procesos en READY")
		<-globals.STSEmpty
		slog.Info("Me desbloqueo en STS porque hay procesos en READY")
		return
	}

	globals.ReadyQueueMutex.Lock()
	isUnique := len(globals.ReadyQueue) == 1
	globals.ReadyQueueMutex.Unlock()

	globals.ReadyQueueMutex.Lock()
	if isUnique {
		slog.Debug("Hay un solo proceso en la ReadyQueue")
		process = globals.ReadyQueue[0]
		globals.ReadyQueue = globals.ReadyQueue[1:]
	} else {
		slog.Debug("Buscando el proceso con menor EstimatedBurst en la ReadyQueue")
		minIndex := 0
		for i := 1; i < len(globals.ReadyQueue); i++ {
			if globals.ReadyQueue[i].EstimatedBurst < globals.ReadyQueue[minIndex].EstimatedBurst {
				minIndex = i
			}
		}
		process = globals.ReadyQueue[minIndex]
		globals.ReadyQueue = append(globals.ReadyQueue[:minIndex], globals.ReadyQueue[minIndex+1:]...)
	}
	globals.ReadyQueueMutex.Unlock()

	globals.CPUsSlotsMu.Lock()

	var minSlot *globals.CPUSlot
	var minBurst float64
	first := true

	for _, slot := range globals.CPUsSlots {
		if slot.Process == nil {
			continue
		}
		if first || slot.Process.EstimatedBurst < minBurst {
			minSlot = slot
			minBurst = slot.Process.EstimatedBurst
			first = false
		}
	}

	globals.CPUsSlotsMu.Unlock()

	forInterrupt := minSlot != nil && minSlot.Process.EstimatedBurst < process.EstimatedBurst

	if !forInterrupt {
		slog.Info("No hay un proceso en ejecución con menor EstimatedBurst que el nuevo proceso, se agrega a la ReadyQueue", "pid", process.PCB.GetPID())
		queue.Enqueue(pcb.READY, process)
		<-globals.WaitingNewProcessInReady
		return
	}

	err := interruptCPU(minSlot.Cpu, minSlot.Process.PCB.GetPID())

	if err != nil {
		slog.Error("Error al interrumpir proceso", "pid", minSlot.Process.PCB.GetPID(), "error", err)
		queue.Enqueue(pcb.READY, process)
		return
	}

	slog.Info("Desalojo ejecutado correctamente", "pid", minSlot.Process.PCB.GetPID())

	slog.Info("SRT envia el proceso ", "pid", process.PCB.GetPID(), " a ejecutar en CPU ", minSlot.Cpu.ID) // ?

	go sendToExecute(process, minSlot.Cpu)
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

	queue.Enqueue(pcb.EXEC, process)

	// ? No deberia llegar a este punto si no hay CPUs disponibles en FIFO y SJF porque se bloquean sí no hay CPUs disponibles, pero dejo esto por SRT
	globals.CPUsSlotsMu.Lock() // ?
	exists := false
	for _, slot := range globals.CPUsSlots {
		if slot.Cpu.ID == cpu.ID {
			exists = true
			slot.Process = process
			break
		}
	}

	if !exists {
		slog.Debug("No existe un slot para la CPU, se crea uno nuevo", "cpuID", cpu.ID)
		slot := new(globals.CPUSlot)
		slot.Cpu = cpu
		slot.Process = process
		globals.CPUsSlots = append(globals.CPUsSlots, slot)
	} else {
		slog.Debug("Se actualiza el slot de la CPU con el nuevo proceso", "cpuID", cpu.ID, "pid", process.PCB.GetPID())
	}

	globals.CPUsSlotsMu.Unlock()

	request := globals.CPURequest{
		PID: process.PCB.GetPID(),
		PC:  process.PCB.GetPC(),
	}

	resp, err := sendToWork(*cpu, request)

	if err != nil {
		// TODO: Y que hago con el proceso?
		globals.AvCPUmu.Lock()
		cpu.Working = false
		globals.AvCPUmu.Unlock()
		slog.Error("Error al enviar el proceso a la CPU", "error", err)
		return
	}

	// TODO: La comunicación entre Kernel y CPU podria ser asincronica.
	switch resp.Motivo {
	case "Interrupt":
		slog.Info("El proceso con el pid %d fue interrumpido por la CPU %d", int(resp.PID), cpu.ID)

		UpdateBurstEstimation(process)

		proc, err := queue.RemoveByPID(pcb.EXEC, process.PCB.GetPID())
		if err != nil {
			slog.Error("Error al remover el proceso de la cola EXEC", "pid", resp.PID, "error", err)
			return
		}
		err = queue.Enqueue(pcb.READY, proc)
		if err != nil {
			slog.Error("Error al re-enqueue el proceso en READY", "pid", proc.PCB.GetPID(), "error", err)
			// ?
		}

		globals.AvCPUmu.Lock()
		cpu.Working = false
		globals.AvCPUmu.Unlock()

                globals.CPUsSlotsMu.Lock()
                for _, slot := range globals.CPUsSlots {
                        if slot.Cpu.ID == cpu.ID {
                                slot.Process = nil
                                break
                        }
                }
                globals.CPUsSlotsMu.Unlock()

		select {
		case globals.CpuBecameIdle <- struct{}{}:
			slog.Debug("Se desbloquea CpuBecameIdle porque una CPU se volvió inactiva")
		default:
			slog.Debug("CpuBecameIdle ya estaba desbloqueada, no se envía señal")
		}

		return
	case "Exit":
		UpdateBurstEstimation(process)

		logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d fue finalizado por la CPU", resp.PID))

		_, err := queue.RemoveByPID(pcb.EXEC, resp.PID)
		if err != nil {
			slog.Error("Error al remover el proceso de la cola EXEC", "pid", resp.PID, "error", err)
			return
		}

		globals.AvCPUmu.Lock()
		cpu.Working = false
		globals.AvCPUmu.Unlock()

		globals.CPUsSlotsMu.Lock() // Igual tengo la referencia al slot arriba.
		for _, slot := range globals.CPUsSlots {
			if slot.Cpu.ID == cpu.ID {
				slot.Process = nil
				break
			}
		}
		globals.CPUsSlotsMu.Unlock()

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
