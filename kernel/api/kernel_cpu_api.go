package kernel_api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"ssoo-kernel/config"
	"ssoo-kernel/globals"
	queue "ssoo-kernel/queues"
	process_shared "ssoo-kernel/shared"
	"ssoo-utils/codeutils"
	"ssoo-utils/httputils"
	"ssoo-utils/pcb"
	"strconv"
	"time"
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

			// !--
			select {
			case globals.NewCpuConnected <- struct{}{}:
				slog.Debug("Nueva CPU añadida. Se desbloquea NewCpuConnected..")
			default:
			}

			select {
			case globals.WaitingNewProcessInReady <- struct{}{}: // ?
				slog.Debug("Replanificando STS")
			default:
			}

			// !--

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
			fmt.Printf("Recibida syscall IO: dispositivo=%s, tiempo=%d\n", device, timeMs)

			deviceFound := false
			/* Ahora mismo, si esta ocupado, sigue con la ejecución, pero se bloquea si la envia.*/
			var selectedIO *globals.IOConnection
			globals.AvIOmu.Lock()
			for i, io := range globals.AvailableIOs {
				if io.Name == device && io.Disp {
					deviceFound = true
					selectedIO = &globals.AvailableIOs[i]
					break
				}
			}
			globals.AvIOmu.Unlock()

			if !deviceFound {

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

				queue.Enqueue(pcb.BLOCKED, process)

				waitIO := new(globals.WaitingIO)
				waitIO.Process = process
				waitIO.IORequest = globals.IORequest{Pid: process.PCB.GetPID(), Timer: timeMs}
				waitIO.IOAvailable = make(chan struct{})

				globals.MTSQueueMu.Lock()
				globals.MTSQueue = append(globals.MTSQueue, waitIO)
				globals.MTSQueueMu.Unlock()

				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Proceso encolado para dispositivo IO ocupado"))
				return
			}
			// Marcar como no disponible y enviar la solicitud
			selectedIO.Disp = false

			go func(io *globals.IOConnection) {

				globals.SendIORequest(process.PCB.GetPID(), timeMs, selectedIO)

				// simula el tiempo de señal de io, asi vuelve a ready
				time.Sleep(time.Duration(timeMs) * time.Millisecond)

				// Liberar dispositivo
				globals.AvIOmu.Lock()
				io.Disp = true
				globals.AvIOmu.Unlock()

				queue.Enqueue(pcb.READY, process)
			}(selectedIO)

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
			EXIT(process)
		default:
			http.Error(w, "Opcode no reconocido", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Syscall procesada"))
	}
}

// !!!!!!!!!!!!!!!!!
func DUMP_MEMORY(process *globals.Process) { // !!!!!!!!!!!!!!!!!
	slog.Info("Syscall DUMP_MEMORY recibida", "pid", process.PCB.GetPID())

	process.PCB.SetState(pcb.BLOCKED) // !!!!!!!!!!!!!!!!!

	go func(p *globals.Process) {
		success := HandleDumpMemory(p)
		if success {
			slog.Info("Proceso desbloqueado tras syscall DUMP_MEMORY exitosa", "pid", p.PCB.GetPID())
			queue.Enqueue(pcb.READY, p)
		} else {
			slog.Error("Proceso pasa a EXIT por fallo en DUMP_MEMORY", "pid", p.PCB.GetPID())
			queue.Enqueue(pcb.EXIT, p)
			process_shared.TerminateProcess(p)
		}
	}(process)
}

// !!!!!!!!!!!!!!!!!

func EXIT(process *globals.Process) {
	queue.Enqueue(pcb.EXIT, process)
	process_shared.TerminateProcess(process)
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
