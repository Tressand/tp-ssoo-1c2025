package main

import (
	"bufio"
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

func main() {
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

	// Create mux
	var mux *http.ServeMux = http.NewServeMux()
	globalCloser := make(chan interface{})
	// Add routes to mux
	mux.Handle("/test", test())

	mux.Handle("/io-notify", handleIO(globalCloser))
	// Pass mux through middleware
	// If it were to happen...

	shutdownSignal := make(chan interface{})
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortKernel, mux, shutdownSignal)

	menu := menu.Create()
	menu.Add("Close Server and Exit Program", func() {
		close(globalCloser)
		shutdownSignal <- struct{}{}
		<-shutdownSignal
		close(shutdownSignal)
		os.Exit(0)
	})
	menu.Add("Liberar IO", func() {
		reader := bufio.NewReader(os.Stdin)
		var target *IOConnection
		fmt.Println("Current available IOs:")
		for _, elem := range availableIOs {
			fmt.Println("	- ", elem.name)
		}
		fmt.Print("Who are we sleeping? (any) ")
		output, _ := reader.ReadString('\n')
		output = output[0 : len(output)-1]
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
		fmt.Printf("Got it. Targetting %s\n", target.name)
		var timer int
		for {
			fmt.Print("How much are we sleeping? (2000ms)")
			output, _ := reader.ReadString('\n')
			output = output[0 : len(output)-1]
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
		target.handler <- timer
		for index, elem := range availableIOs {
			if elem == *target {
				availableIOs = append(availableIOs[:index], availableIOs[index+1:]...)
				break
			}
		}
	})
	for {
		menu.Activate()
	}
}

type IOConnection struct {
	name    string
	handler chan int
}

var avIOmu sync.Mutex
var availableIOs []IOConnection

func handleIO(closer chan interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		name := string(data)
		slog.Info("IO available", "name", name)
		connHandler := make(chan int)
		avIOmu.Lock()
		availableIOs = append(availableIOs, IOConnection{
			name:    name,
			handler: connHandler,
		})
		avIOmu.Unlock()
		select {
		case timer := <-connHandler:
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(fmt.Sprint(timer)))
		case <-closer:
			w.WriteHeader(http.StatusTeapot)
		}
	}
}

func test() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Instance.Info("Test endpoint hit")
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Test recieved."))
		w.WriteHeader(http.StatusOK)
	}
}
