package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"ssoo-utils/httputils"
	"strconv"
	"strings"
	"sync"
)

type CPUConnection struct {
	id      string
	ip      string
	port    int
	handler chan CPURequest
	working bool
}

type CPURequest struct {
	PID uint
	PC  int
}

var connectedCPUs []CPUConnection

func (cpu CPUConnection) SendToWork(request CPURequest) {
	cpu.handler <- request
}

func GetCPUList(working bool) []CPUConnection {
	result := make([]CPUConnection, 0)
	for _, elem := range connectedCPUs {
		if elem.working == working {
			result = append(result, elem)
		}
	}
	return result
}

var avCPUmu sync.Mutex

func ReceiveCPU(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query()

		slog.Info("CPU available", "name", query.Get("id"))

		var thisConnection *CPUConnection
		var alreadyConnected bool = false
		for index, elem := range connectedCPUs {
			if elem.id == query.Get("id") {
				alreadyConnected = true
				thisConnection = &connectedCPUs[index]
				thisConnection.working = false
			}
		}
		if !alreadyConnected {
			port, _ := strconv.Atoi(query.Get("port"))
			connHandler := make(chan CPURequest)
			avCPUmu.Lock()
			connectedCPUs = append(connectedCPUs, CPUConnection{
				id:      query.Get("id"),
				ip:      query.Get("ip"),
				port:    port,
				handler: connHandler,
			})
			thisConnection = &connectedCPUs[len(connectedCPUs)-1]
			avCPUmu.Unlock()
		}

		select {
		case cpuRequest := <-thisConnection.handler:
			thisConnection.working = true
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(fmt.Sprint(cpuRequest)))

		case <-ctx.Done():
			w.WriteHeader(http.StatusTeapot)
		}
	}
}

/*
// Esto después lo voy a preparar para testear!
func AskCPU() {
	list := GetCPUList(false)
	if len(list) == 0 {
		return
	}

	var target *CPUConnection

	// List the name of all CPU's available
	fmt.Println("Current available CPU's:")
	for _, elem := range list {

		fmt.Println("	- ", elem.id)
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
}*/

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
