package main

// #region SECTION: IMPORTS

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	kernel_api "ssoo-kernel/api"
	"ssoo-kernel/config"
	globals "ssoo-kernel/globals"
	process "ssoo-kernel/processes"
	scheduler "ssoo-kernel/scheduler"
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

		process.CreateProcess(pathFile, processSize)
	} else {
		slog.Info("Activando funcionamiento por defecto.")
		process.CreateProcess("helloworld", 300)
	}

	// #endregion
	go scheduler.STS()
	globals.SchedulerStatus = "STOP" // El planificador debe estar frenado por defecto

	// #region CREATE SERVER

	// Create mux
	var mux *http.ServeMux = http.NewServeMux()

	// Closing this context with cancelctx() will trigger a select statement on io connections (see below)
	// Serving as a closer for all established connections.
	ctx, cancelctx := context.WithCancel(context.Background())

	// Add routes to mux
	mux.Handle("/test", test())

	// Pass the globalCloser to handlers that will block.
	mux.Handle("/cpu-notify", kernel_api.ReceiveCPU(ctx))
	mux.Handle("/io-notify", recieveIO(ctx))
	mux.Handle("/syscall", kernel_api.RecieveSyscall())
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
	moduleMenu.Add("[TEST] Create process", func() {
		size := 100 + (rand.Intn(900))
		process.CreateProcess("prueba", size)
	})
	moduleMenu.Add("[TEST] Retry request", func() {
		if globals.SchedulerStatus == "START" {
			globals.RetryProcessCh <- struct{}{}
		}
	})
	moduleMenu.Add("Send IO Signal", sendToIO)
	/*moduleMenu.Add("Send CPU Interrupt", sendInterrupt)
	moduleMenu.Add("Ask CPU to work", askCPU)*/

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

// #region SECTION: TEST ENDPOINT

func test() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Instance.Info("Test endpoint hit")
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Test recieved."))
		w.WriteHeader(http.StatusOK)
	}
}

// #endregion

// #region SECTION: HANDLE CPU CONNECTIONS
// TODO: Lo pase a otro archivo temporalmente

var connectedCPUs []globals.CPUConnection

var avCPUmu sync.Mutex

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

		// Create handler channel and add this IO to the list of available IOs
		connHandler := make(chan globals.IORequest)
		thisConnection := globals.IOConnection{
			Name:    name,
			Handler: connHandler,
			Disp:    true,
		}

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

func sendToIO() {
	if len(globals.AvailableIOs) == 0 {
		return
	}

	var target *globals.IOConnection

	// List the name of all IOs available
	fmt.Println("Current available IOs:")
	for _, elem := range globals.AvailableIOs {
		fmt.Println("	- ", elem.Name)
	}

	fmt.Print("Who are we sleeping? (any) ")
	var output string
	fmt.Scanln(&output)

	// Search for the IO selected
	if output == "" {
		target = &globals.AvailableIOs[0]
	} else {
		for _, io := range globals.AvailableIOs {
			if io.Name == output {
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
	fmt.Printf("Got it. Targetting %s\n", target.Name)
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
	target.Disp = false // esto habria q modificarlo, cosa de que cuando acepte la solicitud recien se ponga en false.
	// Send the timer through the targets channel, this will trigger the recieveIO()'s response.
	globals.SendIORequest(0, timer, target)
}

// #endregion
