package kernel_api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"ssoo-kernel/config"
	"ssoo-kernel/globals"
	"ssoo-kernel/queues"
	queue "ssoo-kernel/queues"
	process_shared "ssoo-kernel/shared"
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

		slog.Info("New CPU connected", "id", id, "ip", ip, "port", port)

		cpu := new(globals.CPUConnection)
		cpu.ID = id
		cpu.IP = ip
		cpu.Port = port
		cpu.Working = false

		globals.AvCPUmu.Lock()
		exists := false
		for _, c := range globals.AvailableCPUs {
			if c.ID == id {
				exists = true
				break
			}
		}
		globals.AvCPUmu.Unlock()

		if !exists {
			globals.AvCPUmu.Lock()
			globals.AvailableCPUs = append(globals.AvailableCPUs, cpu)
			globals.AvCPUmu.Unlock()

			select {
			case globals.CpuAvailableSignal <- struct{}{}:
				slog.Debug("Nueva CPU añadida. Se desbloquea CpuAvailableSignal..")
			default:
			}
		} else {
			slog.Warn("CPU already registered", "id", id)
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("CPU already registered"))
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("CPU registered successfully"))
	}
}

func HandleReason(pid uint, pc int, reason string) {

	process, err := queues.RemoveByPID(pcb.EXEC, pid)

	if err != nil {
		slog.Error("Error al remover proceso de la cola EXEC", "pid", pid, "error", err.Error())
		return
	}

	process_shared.FreeCPU(process)
	process_shared.UpdateBurstEstimation(process)

	switch reason {
	case "Interrupt":
		slog.Info("Procesando interrupción para el proceso", "pid", pid, "pc", pc)
		queue.Enqueue(pcb.READY, process)
	case "Exit":
		logger.Instance.Info(fmt.Sprintf("El proceso con el pid %d fue finalizado por la CPU", pid))
		process_shared.TerminateProcess(process)
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

		go HandleReason(pidUint, pcInt, reason) // ????

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Reason received successfully"))
	}
}

func RecieveSyscall() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cpuID := r.URL.Query().Get("id")

		if cpuID == "" {
			http.Error(w, "Parámetro 'id' requerido", http.StatusBadRequest)
			return
		}

		var process *globals.Process

		globals.CPUsSlotsMu.Lock()
		for _, slot := range globals.CPUsSlots {
			if slot.Cpu.ID == cpuID {
				process = slot.Process
				break
			}
		}
		globals.CPUsSlotsMu.Unlock()

		if process == nil {
			slog.Error("No se encontró el proceso asociado al CPU", "cpuID", cpuID)
			http.Error(w, "No se encontró el proceso asociado al CPU", http.StatusBadRequest)
			return
		}

		// 2. Leer la instrucción del body (en lugar de URL-encoded query param)
		var instruction codeutils.Instruction

		if err := json.NewDecoder(r.Body).Decode(&instruction); err != nil {
			http.Error(w, "Error al parsear JSON de instrucción: "+err.Error(), http.StatusBadRequest)
			return
		}

		defer r.Body.Close()

		// 3. Procesar la syscall con el PID disponible
		opcode := codeutils.Opcode(instruction.Opcode)

		slog.Info(fmt.Sprintf("## (%d) - Solicitó syscall: <%s>", process.PCB.GetPID(), codeutils.OpcodeStrings[opcode]))

		switch opcode {

		case codeutils.IO:
			if len(instruction.Args) != 2 {
				http.Error(w, "IO requiere 2 argumentos", http.StatusBadRequest)
				return
			}

			device := instruction.Args[0]
			timeMs, err := strconv.Atoi(instruction.Args[1])

			if err != nil {
				http.Error(w, "Tiempo invalido", http.StatusBadRequest)
				return
			}

			slog.Debug("Recibida syscall IO: dispositivo=%s, tiempo=%d\n", device, timeMs)

			deviceFound := false
			/* Ahora mismo, si esta ocupado, sigue con la ejecución, pero se bloquea si la envia.*/
			var selectedIO *globals.IOConnection

			globals.AvIOmu.Lock()
			for _, io := range globals.AvailableIOs {
				if io.Name == device {
					deviceFound = true
					selectedIO = io
					break
				}
			}
			globals.AvIOmu.Unlock()

			if !deviceFound {
				process, err = queue.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())

				if err != nil {
					slog.Error("Error al remover proceso de la cola", "pid", process.PCB.GetPID(), "error", err.Error())
					http.Error(w, "Error al remover proceso de la cola", http.StatusInternalServerError)
					return
				}

				process_shared.FreeCPU(process) // Liberar el CPU asociado al proceso

				queue.Enqueue(pcb.EXIT, process)

				process_shared.TerminateProcess(process)

				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Dispositivo no existe - process terminado"))
				return
			}

			if selectedIO == nil {
				http.Error(w, "IO device no disponible o no encontrado", http.StatusServiceUnavailable)
				return
			}

			if !selectedIO.Disp {
				// ????
				process, err = queue.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())

				if err != nil {
					slog.Error("Error al remover proceso de la cola", "pid", process.PCB.GetPID(), "error", err.Error())
					http.Error(w, "Error al remover proceso de la cola", http.StatusInternalServerError)
					return
				}

				process_shared.FreeCPU(process) // Liberar el CPU asociado al proceso

				queue.Enqueue(pcb.BLOCKED, process)

				blockedByIO := new(globals.BlockedByIO)
				blockedByIO.Process = process
				blockedByIO.IOConnection = selectedIO
				blockedByIO.IOName = device
				blockedByIO.IOTime = timeMs
				blockedByIO.TimerStarted = false

				globals.MTSQueueMu.Lock()
				globals.MTSQueue = append(globals.MTSQueue, blockedByIO)
				globals.MTSQueueMu.Unlock()

				globals.MTSEmpty <- struct{}{}
				// ????

				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Proceso encolado para dispositivo IO ocupado"))
				return
			}

			selectedIO.Disp = false

			// ????
			process, err = queue.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())

			if err != nil {
				slog.Error("Error al remover proceso de la cola", "pid", process.PCB.GetPID(), "error", err.Error())
				http.Error(w, "Error al remover proceso de la cola", http.StatusInternalServerError)
				return
			}

			process_shared.FreeCPU(process) // Liberar el CPU asociado al proceso

			queue.Enqueue(pcb.BLOCKED, process)

			blockedByIO := new(globals.BlockedByIO)
			blockedByIO.Process = process
			blockedByIO.IOConnection = selectedIO
			blockedByIO.IOName = device
			blockedByIO.IOTime = timeMs
			blockedByIO.TimerStarted = false

			globals.MTSQueueMu.Lock()
			globals.MTSQueue = append(globals.MTSQueue, blockedByIO)
			globals.MTSQueueMu.Unlock()

			globals.MTSEmpty <- struct{}{}

			// ????

			globals.SendIORequest(process.PCB.GetPID(), timeMs, selectedIO)

			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Operación IO iniciada (en background)"))
			return

			//
		case codeutils.INIT_PROC:
			if len(instruction.Args) != 2 {
				http.Error(w, "IO requiere 2 argumentos", http.StatusBadRequest)
				return
			}
			codePath := instruction.Args[0]
			size, err := strconv.Atoi(instruction.Args[1])
			if err != nil {
				http.Error(w, "tamaño invalido", http.StatusBadRequest)
				return
			}
			process_shared.CreateProcess(codePath, size)
		case codeutils.DUMP_MEMORY:
			DUMP_MEMORY(process)
		case codeutils.EXIT:
			_, err := queue.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())

			if err != nil {
				slog.Error("Error al remover proceso de la cola", "pid", process.PCB.GetPID(), "error", err.Error())
				http.Error(w, "Error al remover proceso de la cola", http.StatusInternalServerError)
				return
			}
			slog.Info("Syscall EXIT recibida", "pid", process.PCB.GetPID())

			process_shared.FreeCPU(process) // Liberar el CPU asociado al proceso

			queue.Enqueue(pcb.EXIT, process)
			process_shared.TerminateProcess(process)
		default:
			http.Error(w, "Opcode no reconocido", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Syscall procesada"))
	}
}

func DUMP_MEMORY(process *globals.Process) {
	_, err := queue.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())

	if err != nil {
		slog.Error("Error al remover proceso de la cola", "pid", process.PCB.GetPID(), "error", err.Error())
		return
	}

	err = queue.Enqueue(pcb.BLOCKED, process)

	if err != nil {
		slog.Error("Error al encolar proceso para DUMP_MEMORY", "pid", process.PCB.GetPID(), "error", err.Error())
		return
	}

	go func(p *globals.Process) {
		success := HandleDumpMemory(p)

		if success {
			slog.Info("Proceso desbloqueado tras syscall DUMP_MEMORY exitosa", "pid", p.PCB.GetPID())

			_, err := queue.RemoveByPID(p.PCB.GetState(), p.PCB.GetPID())

			if err != nil {
				slog.Error("Error al remover proceso de la cola", "pid", p.PCB.GetPID(), "error", err.Error())
				return
			}

			err = queue.Enqueue(pcb.READY, p)

			if err != nil {
				slog.Error("Error al encolar proceso en BLOCKED para DUMP_MEMORY", "pid", p.PCB.GetPID(), "error", err.Error())
				return
			}
		} else {
			slog.Info("Proceso pasa a EXIT por fallo en DUMP_MEMORY", "pid", p.PCB.GetPID())

			_, err := queue.RemoveByPID(p.PCB.GetState(), p.PCB.GetPID())

			if err != nil {
				slog.Error("Error al remover proceso de la cola", "pid", p.PCB.GetPID(), "error", err.Error())
				return
			}

			err = queue.Enqueue(pcb.EXIT, p)

			if err != nil {
				slog.Error("Error al encolar proceso en EXIT para DUMP_MEMORY", "pid", p.PCB.GetPID(), "error", err.Error())
				return
			}

			process_shared.TerminateProcess(p)
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
