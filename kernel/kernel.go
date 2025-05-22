package main

// #region SECTION: IMPORTS

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"

	kernel_api "ssoo-kernel/api"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	processes "ssoo-kernel/processes"
	scheduler "ssoo-kernel/scheduler"
	"ssoo-utils/codeutils"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/menu"
	"ssoo-utils/parsers"
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
		/*
			el cambio adapta las rutas que hizo gero en createProcess, entonces simplemente hacemos ./bin/kernel prueba(obtiene la ruta y la comprueba) 33 (tamanio)
		*/
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

		processes.CreateProcess(pathFile, processSize)
	} else {
		slog.Info("Activando funcionamiento por defecto.")
		processes.CreateProcess("helloworld", 4096)
	}

	// #endregion

	globals.SchedulerStatus = "STOP" // El planificador debe estar frenado por defecto

	// #region CREATE SERVER

	// Create mux
	var mux *http.ServeMux = http.NewServeMux()

	// Closing this context with cancelctx() will trigger a select statement on io connections (see below)
	// Serving as a closer for all established connections.
	ctx, cancelctx := context.WithCancel(context.Background())

	// Pass the globalCloser to handlers that will block.
	mux.Handle("/cpu-notify", kernel_api.ReceiveCPU(ctx))
	mux.Handle("/io-notify", recieveIO(ctx))
	mux.Handle("/syscall", receiveSyscall())
	// Sending anything to this channel will shutdown the server.
	// The server will respond back on this same channel to confirm closing.
	shutdownSignal := make(chan any)
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortKernel, mux, shutdownSignal)

	// #endregion

	// #region MENU

	mainMenu := menu.Create()
	moduleMenu := menu.Create()

	moduleMenu.Add("Init scheduler", func() {
		if globals.SchedulerStatus == "STOP" {
			globals.SchedulerStatus = "START"
			go scheduler.LTS()
			logger.Instance.Info("Scheduler initialized")
		}
	})
	moduleMenu.Add("[TEST] Create processes", func() {
		size := 100 + (rand.Intn(900))
		processes.CreateProcess("prueba", size)
	})
	moduleMenu.Add("[TEST] Retry request", func() {
		if globals.SchedulerStatus == "START" {
			globals.RetryProcessCh <- struct{}{}
		}
	})
	moduleMenu.Add("Send IO Signal", sendToIO)
	moduleMenu.Add("Send CPU Interrupt", func() {
		fmt.Println("Sending CPU interrupt...")
	})
	moduleMenu.Add("Ask CPU to work", func() {
		fmt.Println("Asking CPU to work...")
	})
	moduleMenu.Add("Store value on Memory", func() {
		fmt.Print("Key: ")
		var key string
		var value string
		fmt.Scanln(&key)
		fmt.Print("Value: ")
		fmt.Scanln(&value)

		var url string = httputils.BuildUrl(httputils.URLData{
			Ip:       config.Values.IpMemory,
			Port:     config.Values.PortMemory,
			Endpoint: "storage",
			Queries: map[string]string{
				"key":   key,
				"value": value,
			},
		})

		fmt.Println(url)
		resp, err := http.Post(url, http.MethodPost, http.NoBody)

		fmt.Println(parsers.Struct(resp))
		fmt.Println(err)
	})
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

type IOConnection struct {
	name    string
	handler chan IORequest
}

type IORequest struct {
	pid   uint
	timer int
}

var availableIOs []IOConnection
var avIOmu sync.Mutex

func recieveIO(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Get IO name
		name := r.URL.Query().Get("name")
		slog.Info("IO available", "name", name)

		// Create handler channel and add this IO to the list of available IOs
		connHandler := make(chan IORequest)
		thisConnection := IOConnection{
			name:    name,
			handler: connHandler,
		}

		// Note: Mutex is to prevent race condition on the availableIOs' list
		avIOmu.Lock()
		availableIOs = append(availableIOs, thisConnection)
		avIOmu.Unlock()

		// select will wait for whoever comes first:
		select {
		// A timer is sent through this specific IO handler channel
		case request := <-connHandler:
			// Remove the IOConnection from the list of available ones before sending the response.
			for index, elem := range availableIOs {
				if elem == thisConnection {
					availableIOs = append(availableIOs[:index], availableIOs[index+1:]...)
					break
				}
			}
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(fmt.Sprintf("%d|%d", request.pid, request.timer)))

		// The io context channel closed
		case <-ctx.Done():
			w.WriteHeader(http.StatusTeapot)
		}
	}
}

func sendToIO() {
	if len(availableIOs) == 0 {
		return
	}

	var target *IOConnection

	// List the name of all IOs available
	fmt.Println("Current available IOs:")
	for _, elem := range availableIOs {
		fmt.Println("	- ", elem.name)
	}

	fmt.Print("Who are we sleeping? (any) ")
	var output string
	fmt.Scanln(&output)

	// Search for the IO selected
	if output == "" {
		target = &availableIOs[0]
	} else {
		for _, io := range availableIOs {
			if io.name == output {
				target = &io
				break
			}
		}
	}
	if target == nil {
		fmt.Println("IO not found.")
		return
	}

	// Get the timer
	fmt.Printf("Got it. Targetting %s\n", target.name)
	var timer int
	for {
		fmt.Print("How much are we sleeping? (2000ms)")
		fmt.Scanln(&output)
		if output == "" {
			timer = 2000
			break
		}
		conversion, err := strconv.Atoi(output)
		if err != nil {
			fmt.Print("Lil bro, this not a number...")
			continue
		}
		timer = conversion
		break
	}

	// Send the timer through the targets channel, this will trigger the recieveIO()'s response.
	sendIORequest(0, timer, target)
}

func sendIORequest(pid uint, timer int, io *IOConnection) {
	io.handler <- IORequest{pid: pid, timer: timer}
}

func receiveSyscall() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var instruction codeutils.Instruction

		// Leer el cuerpo del request
		err := json.NewDecoder(r.Body).Decode(&instruction)
		if err != nil {
			http.Error(w, "Error al parsear JSON de instrucción: "+err.Error(), http.StatusBadRequest)
			return
		}

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
			fmt.Printf("Recibida syscall IO: dispositivo=%s, tiempo=%d", device, timeMs)

			for _, io := range availableIOs {
				if io.name == device {
					//logica
					break
				}
			}
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

		default:
			http.Error(w, "Opcode no reconocido", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Syscall procesada"))
	}

}

// #endregion
