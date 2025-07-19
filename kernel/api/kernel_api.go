package kernel_api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"ssoo-kernel/config"
	"ssoo-kernel/globals"
	"ssoo-kernel/queues"
	"ssoo-kernel/shared"
	"ssoo-utils/codeutils"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
	"strconv"
)

func ReceiveCPU() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query()

		id := query.Get("id")
		ip := query.Get("ip")
		port, errPort := strconv.Atoi(query.Get("port"))

		if errPort != nil {
			http.Error(w, "Invalid port", http.StatusBadRequest)
			return
		}

		slog.Info("Recibiendo CPU", "name", id, "ip", ip, "port", port)

		cpu := new(globals.CPUConnection)
		cpu.ID = id
		cpu.IP = ip
		cpu.Port = port
		cpu.Process = nil

		for _, c := range globals.AvailableCPUs {
			if c.ID == id {
				slog.Error("CPU already registered", "id", id)
				w.WriteHeader(http.StatusConflict)
				w.Write([]byte("CPU already registered"))
			}
		}

		globals.AvCPUmu.Lock()
		globals.AvailableCPUs = append(globals.AvailableCPUs, cpu)
		globals.AvCPUmu.Unlock()

		select {
		case globals.CpuAvailableSignal <- struct{}{}:
			slog.Debug("Nueva CPU añadida. Se desbloquea CpuAvailableSignal..")
		default:
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("CPU registered successfully"))
	}
}

func HandleReason(pid uint, pc int, reason string) {

	process := queues.RemoveByPID(pcb.EXEC, pid)

	if process == nil {
		slog.Info("Busqueda erronea en HandleReason", " PID", fmt.Sprint(pid), " Razon", reason)
		return
	}

	fmt.Println(" ")
	fmt.Println(" ")
	slog.Debug("HandleReason", "pid", pid, "pc", pc, "reason", reason)
	fmt.Println(" ")

	process.PCB.SetPC(pc)
	shared.FreeCPU(process)
	globals.UpdateBurstEstimation(process)

	switch reason {
	case "Interrupt":
		logger.RequiredLog(true, pid, "## (%d) - Desalojado por algoritmo", map[string]string{
			"Algoritmo": config.Values.ReadyIngressAlgorithm,
		})
		queues.Enqueue(pcb.READY, process)
	case "Exit":
		logger.RequiredLog(true, pid, "Finaliza el proceso", nil)
		queues.Enqueue(pcb.EXIT, process)
		shared.TerminateProcess(process)
	}
}

func ReceivePidPcReason() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query()

		pid := query.Get("pid")

		pidInt, err := strconv.Atoi(pid)
		if err != nil {
			http.Error(w, "Invalid PID", http.StatusBadRequest)
			return
		}
		pidUint := uint(pidInt)

		pc := query.Get("pc")

		pcInt, err := strconv.Atoi(pc)

		if err != nil {
			http.Error(w, "Invalid PC", http.StatusBadRequest)
			return
		}

		reason := query.Get("reason")

		if reason != "Interrupt" && reason != "Exit" && reason != "" {
			http.Error(w, "Invalid reason", http.StatusBadRequest)
			return
		}

		HandleReason(pidUint, pcInt, reason)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Reason received successfully"))
	}
}

func RecieveSyscall() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		cpuID := r.URL.Query().Get("id")

		fmt.Println(" ")
		fmt.Println("Recibiendo syscall de CPU:", cpuID)
		fmt.Println(" ")

		if cpuID == "" {
			http.Error(w, "Parámetro 'id' requerido", http.StatusBadRequest)
			slog.Error("Parámetro 'id' requerido en Syscall")
			return
		}

		processPC := r.URL.Query().Get("pc")

		if processPC == "" {
			http.Error(w, "Parámetro 'pc' requerido", http.StatusBadRequest)
			slog.Error("Parámetro 'pc' requerido en Syscall")
			return
		}
		processPCInt, err := strconv.Atoi(processPC)
		if err != nil {
			http.Error(w, "Parámetro 'pc' inválido", http.StatusBadRequest)
			slog.Error("Parámetro 'pc' inválido en Syscall", "error", err)
			return
		}

		var process *globals.Process

		for _, cpu := range globals.AvailableCPUs {
			if cpu.ID == cpuID {
				process = cpu.Process
				break
			}
		}

		if process == nil {
			http.Error(w, "No se encontró el proceso asociado al CPU", http.StatusBadRequest)
			slog.Error("No se encontró el proceso asociado al CPU", "cpuID", cpuID)
			return
		}

		process.PCB.SetPC(processPCInt)

		var instruction codeutils.Instruction

		if err := json.NewDecoder(r.Body).Decode(&instruction); err != nil {
			http.Error(w, "Error al parsear JSON de instrucción: "+err.Error(), http.StatusBadRequest)
			slog.Error("Error al parsear JSON de instrucción", "error", err)
			return
		}

		// 3. Procesar la syscall con el PID disponible
		opcode := instruction.Opcode

		logger.RequiredLog(true, process.PCB.GetPID(), "Solicitó Syscall",
			map[string]string{
				"syscall": codeutils.OpcodeStrings[opcode],
				"args":    fmt.Sprintf("%v", instruction.Args),
			})

		switch opcode {
		case codeutils.IO:
			device := instruction.Args[0]
			timeMs, _ := strconv.Atoi(instruction.Args[1])

			iosConNombre := make([]*globals.IOConnection, 0)

			for _, io := range globals.AvailableIOs {
				if io.Name == device {
					iosConNombre = append(iosConNombre, io)
				}
			}

			if len(iosConNombre) == 0 {
				queues.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())
				queues.Enqueue(pcb.EXIT, process)
				shared.TerminateProcess(process)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Dispositivo IO no existe - process terminado"))
				return
			}

			// Buscar una IO disponible
			selectedIO := (*globals.IOConnection)(nil)
			globals.AvIOmu.Lock()
			for _, io := range iosConNombre {
				if io.Disp {
					selectedIO = io
					io.Disp = false
					break
				}
			}
			globals.AvIOmu.Unlock()

			queues.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())
			shared.FreeCPU(process)
			globals.UpdateBurstEstimation(process)

			queues.Enqueue(pcb.BLOCKED, process)
			logger.RequiredLog(true, process.PCB.GetPID(),
				fmt.Sprintf("## (%d) - Bloqueado por IO: %s", process.PCB.GetPID(), device),
				nil)
			blocked := CreateBlocked(process, device, timeMs)

			globals.MTSQueueMu.Lock()
			globals.MTSQueue = append(globals.MTSQueue, blocked)
			globals.MTSQueueMu.Unlock()
			globals.UnlockMTS()

			if selectedIO != nil {
				blocked.Working = true
			} else {
				return
			}

			globals.SendIORequest(process.PCB.GetPID(), timeMs, selectedIO)

		case codeutils.INIT_PROC:
			codePath := instruction.Args[0]
			size, _ := strconv.Atoi(instruction.Args[1])
			shared.CreateProcess(codePath, size)

		case codeutils.DUMP_MEMORY:

			queues.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())
			shared.FreeCPU(process)
			globals.UpdateBurstEstimation(process)

			queues.Enqueue(pcb.BLOCKED, process)
			blocked := CreateBlocked(process, "", 0)
			blocked.Working = true

			globals.MTSQueueMu.Lock()
			globals.MTSQueue = append(globals.MTSQueue, blocked)
			globals.MTSQueueMu.Unlock()

			DUMP_MEMORY(process)

		case codeutils.EXIT:
			process := queues.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())
			queues.Enqueue(pcb.EXIT, process)
			shared.TerminateProcess(process)
			shared.FreeCPU(process)
		default:
			http.Error(w, "Opcode no reconocido", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Syscall " + codeutils.OpcodeStrings[opcode] + " procesada"))
	}
}

func CreateBlocked(process *globals.Process, name string, time int) *globals.Blocked {
	blocked := new(globals.Blocked)
	blocked.Process = process
	blocked.Process.TimerRunning = false
	blocked.Name = name
	blocked.Time = time
	blocked.Working = false
	blocked.DUMP_MEMORY = false //variable para saber si se hace DUMP_MEMORY sobre el proceso
	blocked.CancelTimer = make(chan struct{})
	return blocked
}

func DUMP_MEMORY(process *globals.Process) {

	go func(p *globals.Process) {
		success := HandleDumpMemory(p)

		if success {
			slog.Info("Proceso desbloqueado tras syscall DUMP_MEMORY exitosa", "pid", p.PCB.GetPID())

			removedProcess := queues.RemoveByPID(p.PCB.GetState(), p.PCB.GetPID())

			if removedProcess == nil {
				return
			}

			for i, blocked := range globals.MTSQueue {
				if blocked.Process.PCB.GetPID() == p.PCB.GetPID() {
					globals.MTSQueueMu.Lock()
					globals.MTSQueue = append(globals.MTSQueue[:i], globals.MTSQueue[i+1:]...)
					globals.MTSQueueMu.Unlock()
					break
				}
			}

			queues.Enqueue(pcb.READY, p)

			for _, process := range globals.ReadyQueue {
				slog.Debug("Proceso en READY tras DUMP_MEMORY", "pid", process.PCB.GetPID())
			}

			globals.UnlockMTS()
		} else {
			slog.Info("Proceso pasa a EXIT por fallo en DUMP_MEMORY", "pid", p.PCB.GetPID())

			removedProcess := queues.RemoveByPID(p.PCB.GetState(), p.PCB.GetPID())

			if removedProcess == nil {
				return
			}

			queues.Enqueue(pcb.EXIT, p)
			shared.TerminateProcess(p)
		}
	}(process)
}

func HandleDumpMemory(process *globals.Process) bool {
	pid := process.PCB.GetPID()
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "memory_dump",
		Queries: map[string]string{
			"pid": fmt.Sprint(pid),
		},
	})

	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		slog.Error("Fallo comunicándose con Memoria para DUMP", "pid", pid, "error", err.Error())
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Memoria devolvió error", "pid", pid, "status", resp.StatusCode)
		return false
	}

	slog.Info("DUMP_MEMORY completado", "pid", pid)
	return true
}

func RequestSuspend(process *globals.Process) error {
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
	case globals.MTSEmpty <- struct{}{}:
		slog.Debug("Se desbloquea MTS porque se realizó un swap exitoso")
	default:
	}

	process.InMemory = false

	return nil
}

func Unsuspend(process *globals.Process) bool {

	slog.Debug("Desbloqueando proceso", "pid", process.PCB.GetPID())

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "unsuspend",
		Queries: map[string]string{
			"pid": strconv.Itoa(int(process.PCB.GetPID())),
		},
	})

	resp, err := http.Post(url, "text/plain", nil)
	if err != nil {
		logger.Instance.Error("Error al enviar solicitud de unsuspend", "pid", process.PCB.GetPID(), "error", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Instance.Error("Memoria rechazó la solicitud de unsuspend", "pid", process.PCB.GetPID(), "status", resp.StatusCode)
		return false
	}

	process.InMemory = true
	slog.Info("Solicitud de unsuspend enviada correctamente", "pid", process.PCB.GetPID())

	return true
}
