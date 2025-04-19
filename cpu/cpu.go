package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"ssoo-cpu/config"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/menu"
	"ssoo-utils/parsers"
	"strconv"
)

func main() {
	config.Load()
	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))

	err := logger.SetupDefault("cpu", config.Values.LogLevel)
	defer logger.Close()
	if err != nil {
		fmt.Printf("Error setting up logger: %v\n", err)
		return
	}
	log := logger.Instance
	log.Info("Arranca CPU")

	//
	if len(os.Args) < 2 {
		slog.Error("No CPU ID provided")
		return
	}

	cpuId := os.Args[1]
	//
	var mux *http.ServeMux = http.NewServeMux()

	mux.Handle("/message-from-kernel", messageFromKernel())

	shutdownSignal := make(chan any)
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortCPU, mux, shutdownSignal)

	key, value := getInput()

	url := httputils.BuildUrl(httputils.URLData{
		Base:     config.Values.IpMemory,
		Endpoint: "storage",
		Queries: map[string]string{
			"key":   key,
			"value": value,
		},
	})

	fmt.Printf("Connecting to %s\n", url)
	resp, err := http.Post(url, http.MethodPost, http.NoBody)
	if err != nil {
		slog.Error("POST to Memory failed", "Error", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("POST to Memory status wrong", "status", resp.StatusCode)
		return
	}

	slog.Info("POST to Memory succeded")

	mainMenu := menu.Create()
	moduleMenu := menu.Create()
	moduleMenu.Add("Send id, ip and port to Kernel", func() {
		handshakeWithKernel(cpuId)
	})
	mainMenu.Add("Communicate with other module", func() {
		moduleMenu.Activate()
	})
	mainMenu.Add("Close Server and Exit Program", func() {
		shutdownSignal <- struct{}{}
		<-shutdownSignal
		close(shutdownSignal)
		os.Exit(0)
	})
	for {
		mainMenu.Activate()
	}

}

func handshakeWithKernel(id string) {
	ip := httputils.GetOutboundIP()
	port := strconv.Itoa(config.Values.PortCPU)

	url := httputils.BuildUrl(httputils.URLData{
		Base:     config.Values.IpKernel,
		Endpoint: "cpu-handshake",
		Queries: map[string]string{
			"ip":   ip,
			"port": port,
			"id":   id,
		},
	})
	fmt.Printf("Connecting to %s\n", url)

	resp, err := http.Post(url, http.MethodPost, http.NoBody)
	if err != nil {
		slog.Error("POST to Kernel failed", "Error", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("POST to Kernel status wrong", "status", resp.StatusCode)
		return
	}

	slog.Info("POST to Kernel succeded")
}

func messageFromKernel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Instance.Info("Recibo datos desde kernel")
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Message received."))
		w.WriteHeader(http.StatusOK)
	}
}

func getInput() (string, string) {
	fmt.Print("Key: ")
	var key string
	var value string
	fmt.Scanln(&key)
	fmt.Print("Value: ")
	fmt.Scanln(&value)

	return key, value
}
