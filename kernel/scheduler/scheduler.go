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
		var process *globals.Process
		var err error
		var found bool = false

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

		logger.Instance.Debug("Se intenta inicializar un proceso en LTS", "pid", process.PCB.GetPID())

		process_shared.InititializeProcess(process)
	}
}

func STS() {
	slog.Info("STS iniciado")

	globals.AvCPUmu.Lock()
	noCPUsConnected := len(globals.AvailableCPUs) == 0
	globals.AvCPUmu.Unlock()

	if noCPUsConnected {
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

	usingSRT := config.Values.SchedulerAlgorithm == "SRT"

	for {
		globals.AvCPUmu.Lock()
		availableCPUs := process_shared.GetCPUList(false)
		CPUsAvailable := len(availableCPUs) != 0
		globals.AvCPUmu.Unlock()

		if CPUsAvailable {
			process, readyIsEmpty := queue.Dequeue(pcb.READY, sortBy)

			if readyIsEmpty != nil {
				slog.Info("Se bloquea STS porque no hay procesos en READY")
				<-globals.STSEmpty
				slog.Info("Se desbloquea STS porque hay nuevos procesos en READY")
				continue
			}

			slog.Debug("STS encontró un proceso en READY", "pid", process.PCB.GetPID())

			go sendToExecute(process, availableCPUs[0])
			continue
		}

		if usingSRT {
			process, readyIsEmpty := queue.Search(pcb.READY, sortBy)

			if readyIsEmpty != nil {
				slog.Info("Se bloquea STS porque no hay procesos en READY")
				<-globals.STSEmpty
				slog.Info("Se desbloquea STS porque hay nuevos procesos en READY")
				continue
			}

			globals.CPUsSlotsMu.Lock()

			var minSlot *globals.CPUSlot
			var minBurst float64
			first := true

			for _, slot := range globals.CPUsSlots {
				if slot.Process == nil { // No deberia pasar con SRT
					slog.Debug("slot.Process == nil usando SRT - No deberia entrar aca")
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

			if forInterrupt {
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
		<-globals.CpuAvailableSignal // TODO: investigar sí necesito con buffer
		slog.Debug("Se desbloquea STS porque hay CPUs disponibles")
	}
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
			slot.Cpu.Working = true
			slog.Debug("Se actualiza el slot de la CPU con el proceso", "cpuID", cpu.ID, "pid", process.PCB.GetPID())
			break
		}
	}

	if !exists {
		slog.Debug("No existe un slot para la CPU, se crea uno nuevo", "cpuID", cpu.ID)
		slot := new(globals.CPUSlot)
		slot.Cpu = cpu
		slot.Cpu.Working = true
		slot.Process = process
		globals.CPUsSlots = append(globals.CPUsSlots, slot)
	}

	globals.CPUsSlotsMu.Unlock()

	request := globals.CPURequest{
		PID: process.PCB.GetPID(),
		PC:  process.PCB.GetPC(),
	}

	slog.Debug("Se envia a ejecutar el proceso", "PID", process.PCB.GetPID(), "CPU", cpu.ID)

	err := sendToWork(*cpu, request)

	slog.Debug("La CPU %d termino de trabajar con el proceso con el pid %d", cpu.ID, process.PCB.GetPID())

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

		slog.Error("Error al enviar el proceso a la CPU", "error", err)
		return
	} else {
		slog.Info("Proceso enviado a la CPU correctamente", "pid", process.PCB.GetPID(), "cpuID", cpu.ID)
	}

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

func forSendToWaiting() []*globals.WaitingIO {
	globals.WaitingForIOMu.Lock()
	defer globals.WaitingForIOMu.Unlock()
	list := make([]*globals.WaitingIO, 0)
	for _, elem := range globals.WaitingForIO {
		if elem.Waiting == false {
			list = append(list, elem)
		}
	}
	return list
}

func MTS() {
	for {
		waitingList := forSendToWaiting()

		if len(waitingList) == 0 {
			<-globals.MTSEmpty
			continue
		}

		for _, waiting := range waitingList {
			waiting.Waiting = true
			go sendToWait(waiting)
		}
	}
}

func sendToWait(waiting *globals.WaitingIO) {
	select {
	case waiting.IOSignalAvailable <- struct{}{}:
		slog.Debug("Se desbloquea el proceso esperando IO", "pid", waiting.Process.PCB.GetPID(), "IOName", waiting.IOName)
		waiting.Waiting = false

		process, err := removeProcess(waiting)
		if err != nil {
			slog.Error("Error al remover el proceso de la cola", "pid", waiting.Process.PCB.GetPID(), "error", err)
			return
		}

		queues.Enqueue(pcb.SUSP_BLOCKED, process)

		dispatchToIOHandler(waiting, process.PCB.GetPID())

	case <-time.After(time.Duration(config.Values.SuspensionTime) * time.Millisecond):
		slog.Info("Tiempo de espera para IO agotado. Se mueve de memoria principal a swap", "pid", waiting.Process.PCB.GetPID(), "IOName", waiting.IOName)

		process, err := removeProcess(waiting)
		if err != nil {
			slog.Error("Error al remover el proceso de la cola", "pid", waiting.Process.PCB.GetPID(), "error", err)
			return
		}

		err = queue.Enqueue(pcb.SUSP_BLOCKED, process)
		if err != nil {
			slog.Error("Error al re-enqueue el proceso en SUSP_BLOCKED", "pid", process.PCB.GetPID(), "error", err)
			return
		}

		requestSuspend(process)
	}
}

func removeProcess(waiting *globals.WaitingIO) (*globals.Process, error) {
	return queue.RemoveByPID(waiting.Process.PCB.GetState(), waiting.Process.PCB.GetPID())
}

func dispatchToIOHandler(waiting *globals.WaitingIO, pid uint) {
	globals.AvIOmu.Lock()
	defer globals.AvIOmu.Unlock()

	for _, io := range globals.AvailableIOs {
		if io.Name == waiting.IOName {
			io.Handler <- globals.IORequest{
				Pid:   uint(pid),
				Timer: waiting.IOTime,
			}
			break
		}
	}
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
