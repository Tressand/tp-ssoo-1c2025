package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"ssoo-kernel/config"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/menu"
	"ssoo-utils/parsers"
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
	// Add routes to mux
	mux.Handle("/test", test())
	// Pass mux through middleware
	// If it were to happen...

	shutdownSignal := make(chan interface{})
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortKernel, mux, shutdownSignal)

	menu := menu.Create()
	menu.Add("Close Server and Exit Program", func() {
		shutdownSignal <- struct{}{}
		<-shutdownSignal
		os.Exit(0)
	})
	menu.Add("Pingear Memoria", func() {
		url := config.Values.IpMemory + ":" + fmt.Sprint(config.Values.PortMemory)
		http.Post(url, "text/plain", bytes.NewBuffer([]byte("ping")))
	})
	for {
		menu.Activate()
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
