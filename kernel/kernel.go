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

	// #endregion

	// #region CREATE SERVER

	// Create mux
	var mux *http.ServeMux = http.NewServeMux()

	// Closing this context with cancelctx() will trigger a select statement on io connections (see below)
	// Serving as a closer for all established connections.
	ctx, cancelctx := context.WithCancel(context.Background())

	// Add routes to mux
	mux.Handle("/test", test())

	// Handshake with CPU
	mux.Handle("/cpu-handshake", handshakeCpu())

	// Pass the globalCloser to handlers that will block.
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
	moduleMenu.Add("Send CPU Interrupt", func() {
		fmt.Println("Not implemented")
	})
	moduleMenu.Add("Ask CPU to work", func() {
		fmt.Println("Not implemented")
	})
	moduleMenu.Add("Store value on Memory", func() {
		fmt.Print("Key: ")
		var key string
		var value string
		fmt.Scanln(&key)
		fmt.Print("Value: ")
		fmt.Scanln(&value)

		var url string = httputils.BuildUrl(httputils.URLData{
			Base:     config.Values.IpMemory,
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

// #region SECTION: CPU COMMUNICATION

func handshakeCpu() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get CPU IP and Port
		query := r.URL.Query()
		cpuIp := query.Get("ip")
		cpuPort := query.Get("port")
		cpuId := query.Get("id")

		if cpuIp == "" || cpuPort == "" || cpuId == "" {
			slog.Error("Missing parameters in CPU handshake")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		slog.Info("CPU handshake", "ip", cpuIp, "port", cpuPort, "cpuId", cpuId)

		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Handshake successful"))
		w.WriteHeader(http.StatusOK)
	}
}

// #region SECTION: HANDLE IO CONNECTIONS

type IOConnection struct {
	name    string
	handler chan int
}

var availableIOs []IOConnection
var avIOmu sync.Mutex

func recieveIO(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get IO name
		data, _ := io.ReadAll(r.Body)
		name := string(data)
		slog.Info("IO available", "name", name)

		// Create handler channel and add this IO to the list of available IOs
		connHandler := make(chan int)
		// Note: Mutex is to prevent race condition on the availableIOs' list
		thisConnection := IOConnection{
			name:    name,
			handler: connHandler,
		}
		avIOmu.Lock()
		availableIOs = append(availableIOs, thisConnection)
		avIOmu.Unlock()

		// select will wait for whoever comes first:
		select {
		// A timer is sent through this specific IO handler channel
		case timer := <-connHandler:
			// Remove the IOConnection from the list of available ones before sending the response.
			for index, elem := range availableIOs {
				if elem == thisConnection {
					availableIOs = append(availableIOs[:index], availableIOs[index+1:]...)
					break
				}
			}
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(fmt.Sprint(timer)))

		// The io context channel closed
		case <-ctx.Done():
			w.WriteHeader(http.StatusTeapot)
		}
	}
}

func sendToIO() {
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
	target.handler <- timer
}

// #endregion
