package main

// #region SECTION: IMPORTS

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	kernel_api "ssoo-kernel/api"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	"ssoo-kernel/queues"
	scheduler "ssoo-kernel/scheduler"
	"ssoo-kernel/shared"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/parsers"
	"ssoo-utils/pcb"
	"strconv"
	"syscall"
	"time"
)

// #endregion

// #region SECTION: MAIN

func main() {
	// #region SETUP

	config.Load()

	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))
	err := logger.SetupDefault("kernel", config.Values.LogLevel)
	defer logger.Close()
	if err != nil {
		fmt.Printf("Error setting up logger: %v\n", err)
		return
	}
	log := logger.Instance
	log.Info("Arranca Kernel")

	memoryPing := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "/ping",
	})
	_, err = http.Get(memoryPing)
	if err != nil {
		fmt.Println("Esperando a Memoria")
	}
	for err != nil {
		time.Sleep(1 * time.Second)
		_, err = http.Get(memoryPing)
	}

	if len(os.Args) > 1 {
		if len(os.Args) < 3 {
			fmt.Println("Faltan argumentos! Uso: ./kernel [archivo_pseudocodigo] [tamanio_proceso] [...args]")
			return
		}
		AbsolutepathFile := config.Values.CodeFolder + "/" + os.Args[1]
		if _, err := os.Stat(AbsolutepathFile); os.IsNotExist(err) {
			fmt.Printf("El archivo de pseudocódigo '%s' no existe.\n", AbsolutepathFile)
			return
		}
		pathFile := os.Args[1]

		processSizeStr := os.Args[2]
		processSize, processSizeErr := strconv.Atoi(processSizeStr)

		if processSizeErr != nil {
			fmt.Printf("Error al convertir el tamaño del proceso '%s' a entero: %v\n", processSizeStr, processSizeErr)
			return
		}

		shared.CreateProcess(pathFile, processSize)
	} else {
		slog.Info("Activando funcionamiento por defecto.")
		shared.CreateProcess("helloworld", 300)
	}

	// #endregion

	// #region CREATE SERVER

	// Create mux
	var mux *http.ServeMux = http.NewServeMux()

	// Add routes to mux

	// Pass the globalCloser to handlers that will block.
	mux.Handle("/cpu-notify", kernel_api.ReceiveCPU())
	mux.Handle("/io-notify", recieveIO(globals.IOctx))
	mux.Handle("/io-finished", handleIOFinished())
	mux.Handle("/io-disconnected", handleIODisconnected())
	mux.Handle("/cpu-results", kernel_api.ReceivePidPcReason())
	mux.Handle("/syscall", kernel_api.RecieveSyscall())
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortKernel, mux, globals.ShutdownSignal)

	force_kill_chan := make(chan os.Signal, 1)
	signal.Notify(force_kill_chan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-force_kill_chan
		fmt.Println(sig)
		globals.ClearAndExit()
	}()

	// #endregion

	go scheduler.LTS()
	go scheduler.STS()
	go scheduler.MTS()

	fmt.Println("Presione enter para iniciar el planificador de largo plazo...")
	bufio.NewReader(os.Stdin).ReadString('\n')

	globals.LTSStopped <- struct{}{}

	select {}
}

// #endregion

// #region SECTION: HANDLE IO CONNECTIONS

func getIO(name string, ip string, port int) *globals.IOConnection {
	for _, io := range globals.AvailableIOs {
		if io.Name == name && io.IP == ip && io.Port == port {
			return io
		}
	}
	return nil
}

func recieveIO(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query()

		ip := query.Get("ip")

		if ip == "" {
			http.Error(w, "IP is required", http.StatusBadRequest)
			return
		}

		portStr := query.Get("port")

		if portStr == "" {
			http.Error(w, "Port is required", http.StatusBadRequest)
			return
		}

		port, err := strconv.Atoi(portStr)

		if err != nil {
			http.Error(w, "Invalid port", http.StatusBadRequest)
			return
		}

		name := query.Get("name")

		if name == "" {
			http.Error(w, "Name is required", http.StatusBadRequest)
			return
		}

		var ioConnection *globals.IOConnection = getIO(name, ip, port)

		if ioConnection == nil {
			// If the IO is not available, we create a new IOConnection
			slog.Info("New IO connection", "name", name, "ip", ip, "port", port)

			ioConnection = CreateIOConnection(name, ip, port)

			globals.AvIOmu.Lock()
			globals.AvailableIOs = append(globals.AvailableIOs, ioConnection)
			globals.AvIOmu.Unlock()
		}

		// check if there is a process waiting for this IO

		for _, blocked := range globals.MTSQueue {
			if blocked.Name == name {
				ioConnection.Disp = false
				slog.Info("Found waiting process for IO", "pid", blocked.Process.PCB.GetPID(), "ioName", name)

				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte(fmt.Sprintf("%d|%d", blocked.Process.PCB.GetPID(), blocked.Time)))
				return
			}
		}

		select {
		case request := <-ioConnection.Handler:
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(fmt.Sprintf("%d|%d", request.Pid, request.Timer)))
		case <-ctx.Done():
			w.WriteHeader(http.StatusTeapot)
		}
	}
}

func CreateIOConnection(name string, ip string, port int) *globals.IOConnection {
	ioConnection := new(globals.IOConnection)
	ioConnection.Name = name
	ioConnection.IP = ip
	ioConnection.Port = port
	ioConnection.Handler = make(chan globals.IORequest)
	ioConnection.Disp = true
	return ioConnection
}

func handleIOFinished() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		slog.Info("Handling IO finished")

		query := r.URL.Query()

		ip := query.Get("ip")

		if ip == "" {
			http.Error(w, "IP is required", http.StatusBadRequest)
			return
		}

		portStr := query.Get("port")

		if portStr == "" {
			http.Error(w, "Port is required", http.StatusBadRequest)
			return
		}

		port, err := strconv.Atoi(portStr)

		if err != nil {
			http.Error(w, "Invalid port", http.StatusBadRequest)
			return
		}

		name := query.Get("name")

		if name == "" {
			http.Error(w, "Name is required", http.StatusBadRequest)
			return
		}

		pidStr := query.Get("pid")

		if pidStr == "" {
			http.Error(w, "PID is required", http.StatusBadRequest)
			return
		}

		pid, err := strconv.Atoi(pidStr)

		if err != nil {
			http.Error(w, "Invalid PID", http.StatusBadRequest)
			return
		}

		if io := getIO(name, ip, port); io != nil {
			globals.AvIOmu.Lock()
			io.Disp = true
			globals.AvIOmu.Unlock()
		} else {
			for _, io := range globals.AvailableIOs {
				slog.Info("IO", "name", io.Name, "ip", io.IP, "port", io.Port, "disp", io.Disp)
			}

			http.Error(w, "IO not found", http.StatusNotFound)
			return
		}

		var process *globals.Process

		for i, blocked := range globals.MTSQueue {
			if blocked.Name == name && blocked.Process.PCB.GetPID() == uint(pid) {
				process = blocked.Process
				globals.MTSQueueMu.Lock()
				globals.MTSQueue = append(globals.MTSQueue[:i], globals.MTSQueue[i+1:]...)
				globals.MTSQueueMu.Unlock()
			}
		}

		if process.PCB.GetState() == pcb.SUSP_BLOCKED {
			queues.RemoveByPID(pcb.SUSP_BLOCKED, process.PCB.GetPID())
			queues.Enqueue(pcb.SUSP_READY, process)

			//slog.Info(fmt.Sprintf("## (%d) finalizó IO y pasa a SUSP_READY", process.PCB.GetPID()))
		} else {
			queues.RemoveByPID(pcb.BLOCKED, process.PCB.GetPID())
			queues.Enqueue(pcb.READY, process)
			//slog.Info(fmt.Sprintf("## (%d) finalizó IO y pasa a READY", process.PCB.GetPID()))
		}

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(fmt.Sprintf("IO finished for PID %d", pid)))
	}
}

func handleIODisconnected() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query()

		ip := query.Get("ip")

		if ip == "" {
			http.Error(w, "IP is required", http.StatusBadRequest)
			return
		}

		str_port := query.Get("port")

		if str_port == "" {
			http.Error(w, "Port is required", http.StatusBadRequest)
			return
		}

		port, err := strconv.Atoi(str_port)

		if err != nil {
			http.Error(w, "Invalid Port", http.StatusBadRequest)
			return
		}

		name := query.Get("name")

		if name == "" {
			http.Error(w, "Name is required", http.StatusBadRequest)
			return
		}

		var ioConnection *globals.IOConnection = getIO(name, ip, port)

		if ioConnection == nil {
			http.Error(w, "IO connection not found, this IO was never connected", http.StatusNotFound)
			return
		}

		go func() {
			var indexesToKill []int

			for index, blocked := range globals.MTSQueue {
				if blocked.Name == ioConnection.Name {
					indexesToKill = append(indexesToKill, index)
					break
				}
			}

			for _, index := range indexesToKill {
				process := globals.MTSQueue[index].Process

				globals.MTSQueueMu.Lock()
				globals.MTSQueue = append(globals.MTSQueue[:index], globals.MTSQueue[index+1:]...)
				globals.MTSQueueMu.Unlock()

				queues.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())
				queues.Enqueue(pcb.EXIT, process)
				shared.TerminateProcess(process)
				slog.Info(fmt.Sprintf("Removed process %d from MTS queue due to IO disconnection", process.PCB.GetPID()))
			}

			indexDisconnected := slices.Index(globals.AvailableIOs, ioConnection)
			globals.AvIOmu.Lock()
			globals.AvailableIOs = append(globals.AvailableIOs[:indexDisconnected], globals.AvailableIOs[indexDisconnected+1:]...)
			globals.AvIOmu.Unlock()
		}()

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Handling Disconnection!"))
	}
}

// #endregion
