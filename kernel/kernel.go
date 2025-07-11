package main

// #region SECTION: IMPORTS

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	kernel_api "ssoo-kernel/api"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	"ssoo-kernel/queues"
	scheduler "ssoo-kernel/scheduler"
	process_shared "ssoo-kernel/shared"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/menu"
	"ssoo-utils/parsers"
	"ssoo-utils/pcb"
	"strconv"
	"sync"
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
		process_shared.CreateProcess("proceso3", 300)
	}

	// #endregion

	var wg sync.WaitGroup

	globals.SchedulerStatus = "STOP"

	wg.Add(2)
	go scheduler.LTS()
	go scheduler.STS()

	// #region CREATE SERVER

	// Create mux
	var mux *http.ServeMux = http.NewServeMux()

	// Closing this context with cancelctx() will trigger a select statement on io connections (see below)
	// Serving as a closer for all established connections.
	ctx, cancelctx := context.WithCancel(context.Background())

	// Add routes to mux

	// Pass the globalCloser to handlers that will block.
	mux.Handle("/cpu-notify", kernel_api.ReceiveCPU())
	mux.Handle("/io-notify", recieveIO(ctx))
	mux.Handle("/io-finished", handleIOFinished())
	mux.Handle("/cpu-results", kernel_api.ReceivePidPcReason())
	mux.Handle("/syscall", kernel_api.RecieveSyscall())

	// Sending anything to this channel will shutdown the server.
	// The server will respond back on this same channel to confirm closing.
	shutdownSignal := make(chan any)
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortKernel, mux, shutdownSignal)

	// #endregion

	fmt.Println("Presione enter para iniciar el planificador de largo plazo...")

	bufio.NewReader(os.Stdin).ReadString('\n')
	globals.LTSStopped <- struct{}{}

	wg.Wait()

	// #region MENU

	mainMenu := menu.Create()
	moduleMenu := menu.Create()

	mainMenu.Add("Communicate with other module", func() {
		moduleMenu.Activate()
	})
	mainMenu.Add("Close Server and Exit Program", func() {
		cancelctx()
		shutdownSignal <- struct{}{}
		<-shutdownSignal
		close(shutdownSignal)
		os.Exit(0)
	})
	for {
		mainMenu.Activate()
	}

	// #endregion
}

// #endregion

// #region SECTION: HANDLE IO CONNECTIONS

func recieveIO(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Get IO name
		name := r.URL.Query().Get("name")
		slog.Info("IO available", "name", name)

		// Check if there is a process waiting for this IO

		globals.WaitingForIOMu.Lock()
		for _, waitingIO := range globals.WaitingForIO {
			if waitingIO.IOName == name {
				waitingIO.IOSignalAvailable <- struct{}{}
				slog.Info("Found waiting process for IO", "pid", waitingIO.Process.PCB.GetPID(), "ioName", name)
				globals.WaitingForIOMu.Unlock()

				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte(fmt.Sprintf("%d|%d", waitingIO.Process.PCB.GetPID(), waitingIO.IOTime)))
				return
			}
		}
		globals.WaitingForIOMu.Unlock()

		// !!!!!

		// Create handler channel and add this IO to the list of available IOs
		connHandler := make(chan globals.IORequest)
		thisConnection := new(globals.IOConnection)
		thisConnection.Name = name
		thisConnection.Handler = connHandler
		thisConnection.Disp = true

		// Note: Mutex is to prevent race condition on the availableIOs' list
		globals.AvIOmu.Lock()
		globals.AvailableIOs = append(globals.AvailableIOs, thisConnection)
		globals.AvIOmu.Unlock()

		// select will wait for whoever comes first:
		select {
		// A timer is sent through this specific IO handler channel
		case request := <-connHandler:
			// Remove the IOConnection from the list of available ones before sending the response.
			for index, elem := range globals.AvailableIOs {
				if elem == thisConnection {
					globals.AvailableIOs = append(globals.AvailableIOs[:index], globals.AvailableIOs[index+1:]...)
					break
				}
			}
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(fmt.Sprintf("%d|%d", request.Pid, request.Timer)))

		// The io context channel closed
		case <-ctx.Done():
			w.WriteHeader(http.StatusTeapot)
		}
	}
}

func handleIOFinished() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
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

		process, err := queues.FindByPID(pcb.SUSP_BLOCKED, pidUint)

		if err != nil {
			http.Error(w, fmt.Sprintf("Process with PID %d not found in SUSP_BLOCKED queue", pidInt), http.StatusNotFound)
			return
		}

		process, err = queues.RemoveByPID(process.PCB.GetState(), process.PCB.GetPID())

		if err != nil {
			http.Error(w, fmt.Sprintf("Error removing process with PID %d from queue: %v", pidInt, err), http.StatusInternalServerError)
			return
		}

		queues.Enqueue(pcb.SUSP_READY, process)

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(fmt.Sprintf("IO finished for PID %s", pid)))
	}
}

// #endregion
