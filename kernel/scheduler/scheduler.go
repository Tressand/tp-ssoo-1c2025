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

	if shared.CPUsNotConnected() {
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
		if shared.IsCPUAvailable() {
			cpu := shared.GetAvailableCPU()
			process := queues.Search(pcb.READY, sortBy)

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

			cpu := GetCPUWithShortestBurst()

			interrupt := cpu != nil && cpu.Process.EstimatedBurst > process.EstimatedBurst

			if interrupt {
				err := interruptCPU(cpu, cpu.Process.PCB.GetPID())

				if err != nil {
					slog.Error("Error al interrumpir proceso", "pid", cpu.Process.PCB.GetPID(), "error", err)
					queues.Enqueue(pcb.READY, process)
					return
				}

				process = queues.RemoveByPID(pcb.READY, process.PCB.GetPID())

				if process == nil {
					return
				}

				go sendToExecute(process, cpu)
			}

		}

		slog.Debug("No hay CPUs disponibles, esperando a que se libere una")
		<-globals.CpuAvailableSignal
		slog.Debug("Se desbloquea STS porque hay CPUs disponibles")
	}
}

func ShouldTryInterrupt() bool {
	globals.AvCPUmu.Lock()
	defer globals.AvCPUmu.Unlock()
	return config.Values.SchedulerAlgorithm == "SRT" && len(globals.AvailableCPUs) != 0
}

func GetCPUWithShortestBurst() *globals.CPUConnection {
	globals.AvCPUmu.Lock()
	defer globals.AvCPUmu.Unlock()
	minCPU := globals.AvailableCPUs[0]
	for _, cpu := range globals.AvailableCPUs {
		if cpu.Process != nil && cpu.Process.EstimatedBurst < minCPU.Process.EstimatedBurst {
			minCPU = cpu
		}
	}
	return minCPU
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

	slog.Debug("Se agrega al proceso a EXEC", "pid", process.PCB.GetPID())

	queues.Enqueue(pcb.EXEC, process)

	slog.Debug("Se asocia el proceso a la CPU", "pid", process.PCB.GetPID(), "cpuID", cpu.ID)

	globals.AvCPUmu.Lock()
	cpu.Process = process
	globals.AvCPUmu.Unlock()

	request := globals.CPURequest{
		PID: process.PCB.GetPID(),
		PC:  process.PCB.GetPC(),
	}

	slog.Debug("Se envia a ejecutar el proceso", "PID", process.PCB.GetPID(), "CPU", cpu.ID)

	err := sendToWork(*cpu, request)

	slog.Debug("La CPU %d termino de trabajar con el proceso con el pid %d", cpu.ID, process.PCB.GetPID())

	if err != nil {
		slog.Debug(err.Error())
		process := queues.RemoveByPID(pcb.EXEC, process.PCB.GetPID())

		if process == nil {
			return
		}

		queues.Enqueue(pcb.READY, process)

		globals.AvCPUmu.Lock()
		cpu.Process = nil
		globals.AvCPUmu.Unlock()

		slog.Error("Error al enviar el proceso a la CPU", "error", err)
		return
	}

	slog.Info("Proceso enviado a la CPU correctamente", "pid", process.PCB.GetPID(), "cpuID", cpu.ID)
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
	slog.Debug("Se inicia el timer para el proceso bloqueado por IO", "pid", blocked.Process.PCB.GetPID(), "IOName", blocked.Name)
	time.Sleep(time.Duration(config.Values.SuspensionTime) * time.Millisecond)
	slog.Info("Tiempo de espera para IO agotado. Se mueve de memoria principal a swap", "pid", blocked.Process.PCB.GetPID(), "IOName", blocked.Name)

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
