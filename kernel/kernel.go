package main

// #region SECTION: IMPORTS

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"ssoo-kernel/config"
	process "ssoo-kernel/process"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/menu"
	"ssoo-utils/parsers"
	"strconv"
	"strings"
	"sync"
)

// #endregion

// #region SECTION: MAIN

func main() {
	// load config

	config.Load()

	// #region SETUP

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

	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))
	err := logger.SetupDefault("kernel", config.Values.LogLevel)
	defer logger.Close()
	if err != nil {
		fmt.Printf("Error setting up logger: %v\n", err)
		return
	}
	log := logger.Instance
	log.Info("Arranca Kernel")

	// #endregion

	process.CreateProcess(pathFile, processSize) // ? Va aca o en el menu?
	process.CreateProcess("helloworld", 35)
	// #region CREATE SERVER

	// Create mux
	var mux *http.ServeMux = http.NewServeMux()

	// Closing this context with cancelctx() will trigger a select statement on io connections (see below)
	// Serving as a closer for all established connections.
	ctx, cancelctx := context.WithCancel(context.Background())

	// Add routes to mux
	mux.Handle("/test", test())

	// Pass the globalCloser to handlers that will block.
	mux.Handle("/cpu-notify", receiveCPU(ctx))
	mux.Handle("/io-notify", recieveIO(ctx))

	// Sending anything to this channel will shutdown the server.
	// The server will respond back on this same channel to confirm closing.
	shutdownSignal := make(chan any)
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortKernel, mux, shutdownSignal)

	// #endregion

	// #region MENU

	mainMenu := menu.Create()
	moduleMenu := menu.Create()

	moduleMenu.Add("Send IO Signal", sendToIO)
	moduleMenu.Add("Send CPU Interrupt", sendInterrupt)
	moduleMenu.Add("Ask CPU to work", askCPU)
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

type CPUConnection struct {
	id      string
	ip      string
	port    int
	handler chan int
	working bool
}

var connectedCPUs []CPUConnection

func getCPUList(working bool) []CPUConnection {
	result := make([]CPUConnection, 0)
	for _, elem := range connectedCPUs {
		if elem.working == working {
			result = append(result, elem)
		}
	}
	return result
}

var avCPUmu sync.Mutex

func receiveCPU(ctx context.Context) http.HandlerFunc {
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
			connHandler := make(chan int)
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
		case timer := <-thisConnection.handler:
			thisConnection.working = true
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(fmt.Sprint(timer)))

		case <-ctx.Done():
			w.WriteHeader(http.StatusTeapot)
		}
	}
}

func askCPU() {
	list := getCPUList(false)
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

	// Get the timer
	fmt.Printf("Got it. Targetting %s\n", target.id)
	var timer int
	for {
		fmt.Print("How much are we working? (1m)")
		fmt.Scanln(&output)
		if output == "" {
			timer = 60000
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

	// Send the timer through the targets channel, this will trigger the recieveCPU()'s response.
	target.handler <- timer
}

func sendInterrupt() {
	list := getCPUList(true)
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

// #endregion
