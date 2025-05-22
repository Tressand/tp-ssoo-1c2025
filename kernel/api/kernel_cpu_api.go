package kernel_api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"ssoo-kernel/globals"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"strconv"
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

func ReceiveCPU(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := r.URL.Query().Get("id")
		ip := r.URL.Query().Get("ip")
		portStr := r.URL.Query().Get("port")

		port, err := strconv.Atoi(portStr)
		if err != nil {
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
		defer globals.CpuListMutex.Unlock()

		exists := false
		for _, c := range globals.AvailableCPUs {
			if c.ID == id {
				exists = true
				break
			}
		}

		if !exists {
			globals.AvailableCPUs = append(globals.AvailableCPUs, cpu)
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "CPU %s registered successfully", id)
	}
}

/*
TODO: Necesito preparar algo así para testear STS
func AskCPU() {
	list := GetCPUList(false)
	if len(list) == 0 {
		return
	}

	var target *CPUConnection

	// List the name of all CPU's available
	fmt.Println("Current available CPU's:")
	for _, elem := range list {

		fmt.Println("	- ", elem.ID)
	}

	fmt.Print("Who are we putting to work? (any) ")
	var output string
	fmt.Scanln(&output)

	// Search for the CPU selected
	if output == "" {
		target = &list[0]
	} else {
		for _, io := range list {
			if io.id == output {
				target = &io
				break
			}
		}
	}
	if target == nil {
		fmt.Println("CPU not found.")
		return
	}

	// Send the timer through the targets channel, this will trigger the recieveCPU()'s response.
	target.handler <- CPURequest{}
}

func SendInterrupt() {
	list := GetCPUList(true)
	if len(list) == 0 {
		return
	}

	var target *CPUConnection

	fmt.Println("Current working CPUs:")
	for _, elem := range list {
		fmt.Println("	- ", elem.id)
	}

	fmt.Print("Select CPU to interrupt (any) ")
	var output string
	fmt.Scanln(&output)

	if output == "" {
		target = &list[0]
	} else {
		for _, cpu := range list {
			if cpu.id == output {
				target = &cpu
				break
			}
		}
	}
	if target == nil {
		fmt.Println("CPU not found.")
		return
	}

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       target.ip,
		Port:     target.port,
		Endpoint: "interrupt",
		Queries:  map[string]string{}},
	)

	// Realizar el POST
	resp, err := http.Post(url, "text/plain", strings.NewReader("Interrupt from Kernel"))

	if err != nil {
		fmt.Printf("Error sending interrupt to CPU %s: %v\n", target.id, err)
		return
	}
	defer resp.Body.Close()

	// Leer la respuesta del CPU
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Response from CPU %s: %s\n", target.id, string(body))
}
*/
