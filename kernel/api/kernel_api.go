package kernel_api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"ssoo-kernel/config"
	"ssoo-kernel/globals"
	queue "ssoo-kernel/queues"
	scheduler "ssoo-kernel/scheduler"
	process_shared "ssoo-kernel/shared"
	"ssoo-utils/codeutils"
	"ssoo-utils/httputils"
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

		go scheduler.HandleReason(pidUint, pcInt, reason) // ????

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Reason received successfully"))
	}
}

func FreeCPU(process *globals.Process) {
	globals.CPUsSlotsMu.Lock()
	defer globals.CPUsSlotsMu.Unlock()
	for _, slot := range globals.CPUsSlots {
		if slot.Process == process {
			slot.Process = nil
			slot.Cpu.Working = false

			select {
			case globals.CpuAvailableSignal <- struct{}{}:
				slog.Debug("CPU freed. CpuAvailableSignal unlocked..")
			default:
			}

			break
		}
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

				FreeCPU(process) // Liberar el CPU asociado al proceso

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
				process, err = queue.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())

				if err != nil {
					slog.Error("Error al remover proceso de la cola", "pid", process.PCB.GetPID(), "error", err.Error())
					http.Error(w, "Error al remover proceso de la cola", http.StatusInternalServerError)
					return
				}

				FreeCPU(process) // Liberar el CPU asociado al proceso

				queue.Enqueue(pcb.BLOCKED, process)

				waitIO := new(globals.WaitingIO)
				waitIO.Process = process
				waitIO.IOName = device
				waitIO.IOTime = timeMs
				waitIO.IOSignalAvailable = make(chan struct{})

				globals.WaitingForIOMu.Lock()
				globals.WaitingForIO = append(globals.WaitingForIO, waitIO)
				globals.WaitingForIOMu.Unlock()

				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Proceso encolado para dispositivo IO ocupado"))
				return
			}
			// Marcar como no disponible y enviar la solicitud

			selectedIO.Disp = false // !!!

			process, err = queue.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())

			if err != nil {
				slog.Error("Error al remover proceso de la cola", "pid", process.PCB.GetPID(), "error", err.Error())
				http.Error(w, "Error al remover proceso de la cola", http.StatusInternalServerError)
				return
			}

			FreeCPU(process) // Liberar el CPU asociado al proceso

			queue.Enqueue(pcb.BLOCKED, process)

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

			FreeCPU(process) // Liberar el CPU asociado al proceso

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
