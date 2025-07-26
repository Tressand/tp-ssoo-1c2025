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

	var initialProcessFilename string
	var initialProcessSize int
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
		initialProcessFilename = os.Args[1]

		var err error
		processSizeStr := os.Args[2]
		initialProcessSize, err = strconv.Atoi(processSizeStr)

		if err != nil {
			fmt.Printf("Error al convertir el tamaño del proceso '%s' a entero: %v\n", processSizeStr, err)
			return
		}
	} else {
		slog.Info("Activando funcionamiento por defecto.")
		initialProcessFilename = "helloworld"
		initialProcessSize = 1024
	}

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

	// #endregion

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

	// #endregion

	force_kill_chan := make(chan os.Signal, 1)
	signal.Notify(force_kill_chan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-force_kill_chan
		queues.MostrarLasColas("Finalizacion de Kernel")
		fmt.Println(sig)
		globals.ClearAndExit()
	}()

	shared.CreateProcess(initialProcessFilename, initialProcessSize)

	go scheduler.LTS()
	go scheduler.STS()
	go scheduler.MTS()

	fmt.Print("\nPresione enter para iniciar el planificador de largo plazo...\n\n")
	bufio.NewReader(os.Stdin).ReadString('\n')

	globals.LTSStopped <- struct{}{}

	select {}
}

// #endregion

// #region SECTION: HANDLE IO CONNECTIONS

func getIO(name string, ip string, port string) *globals.IOConnection {
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

		port := query.Get("port")

		name := query.Get("name")

		if name == "" {
			http.Error(w, "Name is required", http.StatusBadRequest)
			return
		}

		var ioConnection *globals.IOConnection = getIO(name, ip, port)

		if ioConnection == nil {
			// If the IO is not available, we create a new IOConnection
			slog.Info("Creada nueva instancia", "name", name)

			ioConnection = CreateIOConnection(name, ip, port)
			slog.Info("Instancia IO","Contenido",fmt.Sprint(ioConnection),"Puerto",port)

			globals.AvIOmu.Lock()
			globals.AvailableIOs = append(globals.AvailableIOs, ioConnection)
			globals.AvIOmu.Unlock()
		}

		// check if there is a process waiting for this IO

		for _, blocked := range globals.MTSQueue {
			if blocked.Name == name && !blocked.Working {
				ioConnection.Disp = false
				blocked.Working = true
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

func CreateIOConnection(name string, ip string, port string) *globals.IOConnection {
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

		query := r.URL.Query()

		ip := query.Get("ip")

		if ip == "" {
			http.Error(w, "IP is required", http.StatusBadRequest)
			return
		}

		port := query.Get("port")

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

		slog.Info("Handling IO finished", "name", name, "pid", pid, " ip", ip, "port", port)
		logger.RequiredLog(true, uint(pid),
			fmt.Sprintf("## (%d) finalizó IO y pasa a READY", pid), nil)

		var process *globals.Process

		globals.UnsuspendMutex.Lock()
		defer globals.UnsuspendMutex.Unlock()
		for i, blocked := range globals.MTSQueue {
			if blocked.Name == name && blocked.Process.PCB.GetPID() == uint(pid) {
				process = blocked.Process

				globals.MTSQueueMu.Lock()
				globals.MTSQueue = append(globals.MTSQueue[:i], globals.MTSQueue[i+1:]...)
				globals.MTSQueueMu.Unlock()
			}
		}

		if process == nil {
			slog.Error("No se encontró el proceso en MTSQueue para IO finished", "name", name, "pid", pid)
			http.Error(w, "Process not found for IO finished", http.StatusNotFound)
			return
		}

		if process.PCB.GetState() == pcb.SUSP_BLOCKED {
			queues.RemoveByPID(pcb.SUSP_BLOCKED, process.PCB.GetPID())
			queues.Enqueue(pcb.SUSP_READY, process)
			globals.UnlockMTS()
		} else if process.PCB.GetState() == pcb.BLOCKED {
			queues.RemoveByPID(pcb.BLOCKED, process.PCB.GetPID())
			queues.Enqueue(pcb.READY, process)
			globals.UnlockSTS()
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

		port := query.Get("port")

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

		var ioConnection *globals.IOConnection = getIO(name, ip, port)

		if ioConnection == nil {
			http.Error(w, "IO connection not found, this IO was never connected", http.StatusNotFound)
			return
		}

		for _, blocked := range globals.MTSQueue {
			if blocked.Name == name && blocked.Process.PCB.GetPID() == uint(pid) {

				process := blocked.Process
				globals.RemoveBlockedByPID(blocked.Process.PCB.GetPID())
				queues.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())
				queues.Enqueue(pcb.EXIT, process)
				shared.TerminateProcess(process)
				slog.Info(fmt.Sprintf("Removed process %d from MTS queue due to IO disconnection", process.PCB.GetPID()))
				break
			}
		}

		indexDisconnected := slices.Index(globals.AvailableIOs, ioConnection)
		globals.AvIOmu.Lock()
		globals.AvailableIOs = append(globals.AvailableIOs[:indexDisconnected], globals.AvailableIOs[indexDisconnected+1:]...)
		globals.AvIOmu.Unlock()

		found := false

		for _, instance := range globals.AvailableIOs {
			if instance.Name == name {
				found = true
			}
		}

		if found {
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("Handling Disconnection!"))
			return
		}

		go func() {
			indexesToKill := make([]int, 0)
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

			for index,_ := range globals.SuspReadyQueue {
				process := globals.SuspReadyQueue[index]

				globals.MTSQueueMu.Lock()
				globals.SuspReadyQueue = append(globals.SuspReadyQueue[:index], globals.SuspReadyQueue[index+1:]...)
				globals.MTSQueueMu.Unlock()

				queues.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())
				queues.Enqueue(pcb.EXIT, process)
				shared.TerminateProcess(process)
				slog.Info(fmt.Sprintf("Removed process %d from Suspend Ready queue due to IO disconnection", process.PCB.GetPID()))
			}

		}()

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Handling Disconnection!"))
	}
}

// #endregion
