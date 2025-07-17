package scheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"ssoo-kernel/api"
	"ssoo-kernel/config"
	"ssoo-kernel/globals"
	"ssoo-kernel/queues"
	"ssoo-kernel/shared"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
	"time"
)

//#region LTS

func LTS() {
	<-globals.LTSStopped
	var sortBy queues.SortBy

	switch config.Values.ReadyIngressAlgorithm {
	case "FIFO":
		sortBy = queues.NoSort
	case "PMCP":
		sortBy = queues.Size
	default:
		panic("algoritmo de largo plazo inválido")
	}

	for {

		var process = queues.Dequeue(pcb.NEW, sortBy)
		
		if process == nil {
			slog.Info("No hay procesos pendientes. Se bloquea hasta que se agregen procesos nuevos")
			select{
				case globals.STSEmpty <- struct{}{}:
					slog.Debug("Se desbloquea STS porque hay nuevos procesos en READY")
				default:
			}
			<-globals.LTSEmpty
			continue
		}
		
		slog.Debug("Se encontró un proceso pendiente, inicializando...", "pid", process.PCB.GetPID())
		shared.InititializeProcess(process)
	}
}

//#endregion
//#region STS

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

			slog.Info("CPU disponible, asignando proceso")
			cpu := shared.GetAvailableCPU()
			process := queues.Dequeue(pcb.READY, sortBy)

			if process == nil {
				slog.Info("Se bloquea STS porque no hay procesos en READY")
				<-globals.STSEmpty
				continue
			}
			slog.Info("Proceso encontrado en READY", "pid", process.PCB.GetPID())

			sendToExecute(process, cpu)
			continue
		}

		if ShouldTryInterrupt() {

			slog.Info("Se analiza Interrupción de CPU")

			process := queues.Search(pcb.READY, sortBy)

			if process == nil {
				slog.Info("Se bloquea STS porque no hay procesos en READY")
				<-globals.STSEmpty
				continue
			}

			cpu := GetCPUWithLonguesBurst()

			interrupt := cpu != nil && globals.TiempoRestanteDeRafaga(cpu.Process) > process.EstimatedBurst

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

				sendToExecute(process, cpu)
			}

		}

		slog.Debug("No hay CPUs disponibles, esperando a que se libere una")
		<-globals.CpuAvailableSignal
		slog.Debug("Se desbloquea STS porque hay CPUs disponibles")
	}
}

//#endregion

func ShouldTryInterrupt() bool {
	return config.Values.SchedulerAlgorithm == "SRT" && len(globals.AvailableCPUs) != 0
}

func GetCPUWithLonguesBurst() *globals.CPUConnection {

	maxCPU := globals.AvailableCPUs[0]
	for _, cpu := range globals.AvailableCPUs {
		if cpu.Process != nil && globals.TiempoRestanteDeRafaga(cpu.Process) > globals.TiempoRestanteDeRafaga(maxCPU.Process) {
			maxCPU = cpu
		}
	}
	return maxCPU
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

	if process.PCB.GetState() == pcb.EXIT {
		slog.Error("Intento de asignar proceso en EXIT a CPU", "pid", process.PCB.GetPID())
		return
	}

	fmt.Println()
	logger.RequiredLog(true, process.PCB.GetPID(),
		fmt.Sprintf("Pasa del estado %s al estado %s",pcb.READY.String(), pcb.EXEC.String()),
		map[string]string{
			"CPU": cpu.ID,
		},
	)
	for _,exec := range globals.ExecQueue{
		slog.Debug("Ejecutando proceso en CPU", "pid", exec.PCB.GetPID())
	}

	queues.Enqueue(pcb.EXEC, process)

	globals.AvCPUmu.Lock()
	cpu.Process = process
	globals.AvCPUmu.Unlock()

	request := globals.CPURequest{
		PID: process.PCB.GetPID(),
		PC:  process.PCB.GetPC(),
	}

	process.StartTime = time.Now()

	err := sendToWork(*cpu, request)

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

//#region MTS

func MTS() {

	var sortBy queues.SortBy
	var noMemory = false

	switch config.Values.ReadyIngressAlgorithm {
	case "FIFO":
		sortBy = queues.NoSort
	case "PMCP":
		sortBy = queues.Size
	default:
		panic("algoritmo de largo plazo inválido")
	}

	for {

		for _, blocked := range globals.MTSQueue {

			if !blocked.Process.TimerStarted  && !blocked.DUMP_MEMORY{//evitar que se inicie el timer si ya se hizo DUMP_MEMORY
				blocked.Process.TimerStarted = true
				go sendToWait(blocked)
			}
		}

		for {
			process := queues.Dequeue(pcb.SUSP_READY, sortBy)

			if process == nil {
				slog.Info("No hay procesos pendientes en SUSP_READY. Se bloquea MTS")
				break
			}

			if kernel_api.Unsuspend(process){
				globals.RemoveBlockedByPID(process.PCB.GetPID())
				queues.RemoveByPID(pcb.SUSP_READY, process.PCB.GetPID())
				queues.Enqueue(pcb.READY, process)
				globals.UnlockSTS()	
			} else{
				noMemory = true
				break
			}
		}

		if !noMemory {
			select {
				case globals.RetryInitialization <- struct{}{}:
					slog.Debug("Se desbloquea LTS porque hay procesos en NEW")
				case globals.LTSEmpty <- struct{}{}:
					slog.Debug("Se desbloquea LTS porque hay procesos en SUSP_READY")
				default:
			}

		} else {
			noMemory = false
		}

		<-globals.MTSEmpty
	}
}
//#endregion

func sendToWait(blocked *globals.Blocked) {
	slog.Debug("Se inicia el timer para el proceso bloqueado por IO", "pid", blocked.Process.PCB.GetPID(), "IOName", blocked.Name)
	time.Sleep(time.Duration(config.Values.SuspensionTime) * time.Millisecond)

	if blocked.Process.PCB.GetState() != pcb.BLOCKED {
		return
	}

	slog.Info("Tiempo de espera para IO agotado. Se mueve de memoria principal a swap", "pid", blocked.Process.PCB.GetPID(), "IOName", blocked.Name)

	process := queues.RemoveByPID(blocked.Process.PCB.GetState(), blocked.Process.PCB.GetPID())

	if process == nil {
		return
	}

	queues.Enqueue(pcb.SUSP_BLOCKED, process)

	kernel_api.RequestSuspend(process)
}