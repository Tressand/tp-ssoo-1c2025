package kernel_api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"ssoo-kernel/globals"
	"ssoo-kernel/processes"
	"ssoo-utils/codeutils"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
	"strconv"
	"time"
)

func GetCPUList(working bool) []*globals.CPUConnection {
	globals.CpuListMutex.Lock()
	defer globals.CpuListMutex.Unlock()
	result := make([]*globals.CPUConnection, 0)
	for i := range globals.AvailableCPUs {
		if globals.AvailableCPUs[i].Working == working {
			result = append(result, &globals.AvailableCPUs[i])
		}
	}
	return result
}

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

		cpu := globals.CPUConnection{
			ID:   id,
			IP:   ip,
			Port: port,
		}

		globals.CpuListMutex.Lock()
		exists := false
		for _, c := range globals.AvailableCPUs {
			if c.ID == id {
				exists = true
				break
			}
		}
		globals.CpuListMutex.Unlock()

		if !exists {
			globals.CpuListMutex.Lock()
			globals.AvailableCPUs = append(globals.AvailableCPUs, cpu)
			globals.CpuListMutex.Unlock()

			if globals.WaitingForCPU {
				globals.AvailableCpu <- struct{}{}
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

func RecieveSyscall() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. Obtener el PID del query parameter
		cpuID := r.URL.Query().Get("id")
		if cpuID == "" {
			http.Error(w, "Parámetro 'id' requerido", http.StatusBadRequest)
			return
		}
		proceso, err := processes.SearchProcessWorking(cpuID)

		if err != nil {
			fmt.Print("No se pudo encontrar la CPU")
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

				lastState := proceso.PCB.GetState()
				proceso.PCB.SetState(pcb.EXIT)
				actualState := proceso.PCB.GetState()
				logger.RequiredLog(true, proceso.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})

				processes.TerminateProcess(proceso)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Dispositivo no existe - proceso terminado"))
				return
			}
			if selectedIO == nil {
				http.Error(w, "IO device no disponible o no encontrado", http.StatusServiceUnavailable)
				return
			}

			if !selectedIO.Disp {
				lastState := proceso.PCB.GetState()
				proceso.PCB.SetState(pcb.BLOCKED)
				actualState := proceso.PCB.GetState()
				logger.RequiredLog(true, proceso.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})

				blockedProcess := globals.BlockedProcess{
					Process:   *proceso,
					IORequest: globals.IORequest{Pid: proceso.PCB.GetPID(), Timer: timeMs},
				}
				globals.BlockedQueue = append(globals.BlockedQueue, blockedProcess)

				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Proceso encolado para dispositivo IO ocupado"))
			}
			// Marcar como no disponible y enviar la solicitud
			selectedIO.Disp = false

			go func(io *globals.IOConnection) {

				globals.SendIORequest(proceso.PCB.GetPID(), timeMs, selectedIO)

				// simula el tiempo de señal de io, asi vuelve a ready
				time.Sleep(time.Duration(timeMs) * time.Millisecond)

				// Liberar dispositivo
				globals.AvIOmu.Lock()
				io.Disp = true
				globals.AvIOmu.Unlock()

				// Reactivar proceso
				proceso.PCB.SetState(1) // READY
				globals.STS = append(globals.LTS, *proceso)
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
			processes.CreateProcess(codePath, size)

		case codeutils.DUMP_MEMORY:

		case codeutils.EXIT:

			lastState := proceso.PCB.GetState()
			proceso.PCB.SetState(pcb.EXIT)
			actualState := proceso.PCB.GetState()
			logger.RequiredLog(true, proceso.PCB.GetPID(), fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()), map[string]string{})

			processes.TerminateProcess(proceso)

		default:
			http.Error(w, "Opcode no reconocido", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Syscall procesada"))
	}
}
