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
	kernel_api "ssoo-kernel/api"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	"ssoo-kernel/queues"
	scheduler "ssoo-kernel/scheduler"
	process_shared "ssoo-kernel/shared"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/parsers"
	"ssoo-utils/pcb"
	"strconv"
	"sync"
	"syscall"
)

// #endregion

// #region SECTION: MAIN

var ioctx, cancelioctx = context.WithCancel(context.Background())

func clearAndExit(shutdownSignal chan any) {
	fmt.Println("Cerrando Kernel...")
	cancelioctx()
	globals.Clear()
	shutdownSignal <- struct{}{}
	<-shutdownSignal
	close(shutdownSignal)
	os.Exit(0)
}

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

		process_shared.CreateProcess(pathFile, processSize)
	} else {
		slog.Info("Activando funcionamiento por defecto.")
		process_shared.CreateProcess("helloworld", 300)
	}

	// #endregion

	var wg sync.WaitGroup

	globals.SchedulerStatus = "STOP"

	wg.Add(3)
	go scheduler.LTS()
	go scheduler.STS()
	go scheduler.MTS()

	// #region CREATE SERVER

	// Create mux
	var mux *http.ServeMux = http.NewServeMux()

	// Add routes to mux

	// Pass the globalCloser to handlers that will block.
	mux.Handle("/cpu-notify", kernel_api.ReceiveCPU())
	mux.Handle("/io-notify", recieveIO(ioctx))
	mux.Handle("/io-finished", handleIOFinished())
	mux.Handle("/io-disconnected", handleIODisconnected())
	mux.Handle("/cpu-results", kernel_api.ReceivePidPcReason())
	mux.Handle("/syscall", kernel_api.RecieveSyscall())

	// Sending anything to this channel will shutdown the server.
	// The server will respond back on this same channel to confirm closing.
	shutdownSignal := make(chan any)
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortKernel, mux, shutdownSignal)

	force_kill_chan := make(chan os.Signal, 1)
	signal.Notify(force_kill_chan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-force_kill_chan
		fmt.Println(sig)
		clearAndExit(shutdownSignal)
	}()

	// #endregion

	fmt.Println("Presione enter para iniciar el planificador de largo plazo...")
	bufio.NewReader(os.Stdin).ReadString('\n')

	globals.LTSStopped <- struct{}{}

	wg.Wait()
	clearAndExit(shutdownSignal)
}

// #endregion

// #region SECTION: HANDLE IO CONNECTIONS

func hasIO(name string, ip string, port string) bool {
	globals.AvIOmu.Lock()
	defer globals.AvIOmu.Unlock()

	intPort, err := strconv.Atoi(port)

	if err != nil {
		slog.Error("Error converting port to int", "port", port, "error", err)
		return false
	}

	for _, io := range globals.AvailableIOs {
		if io.Name == name && io.IP == ip && io.Port == intPort {
			return true
		}
	}
	return false
}

func getIoConnection(name string, ip string, port int) *globals.IOConnection {
	globals.AvIOmu.Lock()
	defer globals.AvIOmu.Unlock()

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

		if port == "" {
			http.Error(w, "Port is required", http.StatusBadRequest)
			return
		}

		portInt, err := strconv.Atoi(port)

		if err != nil {
			http.Error(w, "Invalid port", http.StatusBadRequest)
			return
		}

		name := query.Get("name")

		if name == "" {
			http.Error(w, "Name is required", http.StatusBadRequest)
			return
		}

		var ioConnection *globals.IOConnection

		if !hasIO(name, ip, port) {
			// If the IO is not available, we create a new IOConnection
			slog.Info("New IO connection", "name", name, "ip", ip, "port", port)

			ioConnection = new(globals.IOConnection)
			ioConnection.Name = name
			ioConnection.IP = ip
			ioConnection.Port = portInt
			ioConnection.Handler = make(chan globals.IORequest)
			ioConnection.Disp = true

			globals.AvIOmu.Lock()
			globals.AvailableIOs = append(globals.AvailableIOs, ioConnection)
			globals.AvIOmu.Unlock()
		} else {
			ioConnection = getIoConnection(name, ip, portInt)
		}

		// check if there is a process waiting for this IO

		globals.MTSQueueMu.Lock()
		for _, blocked := range globals.MTSQueue {
			if blocked.IOName == name && blocked.TimerStarted {
				blocked.IOConnection = ioConnection
				blocked.IOConnection.Disp = false
				slog.Info("Found waiting process for IO", "pid", blocked.Process.PCB.GetPID(), "ioName", name)
				globals.MTSQueueMu.Unlock()

				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte(fmt.Sprintf("%d|%d", blocked.Process.PCB.GetPID(), blocked.IOTime)))
				return
			}
		}
		globals.MTSQueueMu.Unlock()

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

func handleIOFinished() http.HandlerFunc { /// ??? Tendre que verificar que también sea la misma ip y puerto??
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		name := r.URL.Query().Get("name")

		if name == "" {
			http.Error(w, "IO name is required", http.StatusBadRequest)
			return
		}

		pid := r.URL.Query().Get("pid")

		if pid == "" {
			http.Error(w, "PID is required", http.StatusBadRequest)
			return
		}

		pidInt, err := strconv.Atoi(pid)
		if err != nil {
			http.Error(w, "Invalid PID", http.StatusBadRequest)
			return
		}

		pidUint := uint(pidInt)

		var process *globals.Process

		globals.MTSQueueMu.Lock()

		for index, blocked := range globals.MTSQueue {
			if blocked.IOName == name && blocked.Process.PCB.GetPID() == pidUint { // ????
				process = blocked.Process
				blocked.IOConnection.Disp = true
				globals.MTSQueue = append(globals.MTSQueue[:index], globals.MTSQueue[index+1:]...)
				break
			}
		}

		globals.MTSQueueMu.Unlock()

		if process.PCB.GetState() == pcb.SUSP_BLOCKED {
			queues.RemoveByPID(pcb.SUSP_BLOCKED, process.PCB.GetPID())
			queues.Enqueue(pcb.SUSP_READY, process)
			slog.Info(fmt.Sprintf("## (%d) finalizó IO y pasa a SUSP_READY", process.PCB.GetPID()))
		} else {
			queues.RemoveByPID(pcb.BLOCKED, process.PCB.GetPID())
			queues.Enqueue(pcb.READY, process)
			slog.Info(fmt.Sprintf("## (%d) finalizó IO y pasa a READY", process.PCB.GetPID()))
		}

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(fmt.Sprintf("IO finished for PID %s", pid)))
	}
}

/// TODO: Terminar IO connection when the IO is disconnected

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

		if port == "" {
			http.Error(w, "Port is required", http.StatusBadRequest)
			return
		}

		portInt, err := strconv.Atoi(port)

		if err != nil {
			http.Error(w, "Invalid port", http.StatusBadRequest)
			return
		}

		name := query.Get("name")

		if name == "" {
			http.Error(w, "Name is required", http.StatusBadRequest)
			return
		}

		var ioConnection *globals.IOConnection

		if !hasIO(name, ip, port) {
			http.Error(w, "IO connection not found", http.StatusNotFound)
			return
		}

		globals.AvIOmu.Lock()
		for index, io := range globals.AvailableIOs {
			if io.Name == name && io.IP == ip && io.Port == portInt {
				ioConnection = io
				globals.AvailableIOs = append(globals.AvailableIOs[:index], globals.AvailableIOs[index+1:]...)
			}
		}
		globals.AvIOmu.Unlock()

		if ioConnection == nil {
			http.Error(w, "IO connection not found", http.StatusNotFound)
			return
		}

		var process *globals.Process

		globals.MTSQueueMu.Lock()
		for index, blocked := range globals.MTSQueue {
			if blocked.IOConnection == ioConnection {
				process = blocked.Process
				globals.MTSQueue = append(globals.MTSQueue[:index], globals.MTSQueue[index+1:]...)
				break
			}
		}
		globals.MTSQueueMu.Unlock()

		if process != nil {
			queues.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())
			queues.Enqueue(pcb.EXIT, process)
			process_shared.TerminateProcess(process)
		}

		// TODO: Antes de hacer lo de abajo, revisar sí los procesos en blocked estan esperando una IOConnection especifica o una IO de un nombre específico.
		// TODO: Tienen que hacer esto ultimo,.
		// TODO: Revisar sí hay más IOs con el mismo nombre y si no hay, buscar todos los procesos que estaban esperando por este IO y moverlos a EXIT

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("IO disconnected successfully!"))
	}
}

// #endregion
