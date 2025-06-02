package kernel_api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"ssoo-kernel/globals"
	"ssoo-kernel/processes"
	"ssoo-utils/codeutils"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"strconv"
	"time"
)

func SendToWork(cpu globals.CPUConnection, request globals.CPURequest) (globals.DispatchResponse, error) {
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

func GetCPUList(working bool) []globals.CPUConnection {
	globals.CpuListMutex.Lock()
	defer globals.CpuListMutex.Unlock()
	result := make([]globals.CPUConnection, 0)
	for _, elem := range globals.AvailableCPUs {
		if elem.Working == working {
			result = append(result, elem)
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
		proceso, err := processes.SearchProcessInExec(cpuID)

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
				proceso.PCB.SetState(4)
				globals.MTS = append(globals.MTS, *proceso)

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

			processes.TerminateProcess(proceso)

		default:
			http.Error(w, "Opcode no reconocido", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Syscall procesada"))
	}
}
