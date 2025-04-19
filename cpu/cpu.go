package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"ssoo-cpu/config"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/menu"
	"ssoo-utils/parsers"
	"strconv"
	"sync"
	"time"
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

	var mux *http.ServeMux = http.NewServeMux()

	mux.Handle("/interrupt", interrupt())

	shutdownSignal := make(chan any)
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortCPU, mux, shutdownSignal)

	var wg sync.WaitGroup
	ctx, cancelctx := context.WithCancel(context.Background())

	wg.Add(1)
	go createKernelConnection("CPU1", 3, 5, &wg, ctx)

	mainMenu := menu.Create()
	mainMenu.Add("Store value on Memory", func() { sendValueToMemory(getInput()) })
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
}

func sendValueToMemory(key string, value string) {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
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
}

func createKernelConnection(
	name string,
	retryAmount int,
	retrySeconds int,
	wg *sync.WaitGroup,
	ctx context.Context) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			retry, err := notifyKernel(name, ctx)
			if !retry {
				return
			}
			if err != nil {
				if retryAmount <= 0 {
					return
				}
				time.Sleep(time.Duration(retrySeconds) * time.Second)
				retryAmount--
			}
		}
	}
}

func notifyKernel(id string, ctx context.Context) (bool, error) {
	log := slog.With("name", id)
	log.Info("Notificando a Kernel...")

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "cpu-notify",
		Queries: map[string]string{
			"ip":   httputils.GetOutboundIP(),
			"port": fmt.Sprint(config.Values.PortCPU),
			"id":   id,
		},
	})

	resp, err := http.Post(url, http.MethodPost, http.NoBody)

	if err != nil {
		fmt.Println("Probably the server is not running, logging error")
		log.Error("Error making POST request", "error", err)
		return true, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTeapot {
			log.Info("Server asked for shutdown.")
			return false, nil
		}
		log.Error("Error on response", "Status", resp.StatusCode, "error", err)
		return true, fmt.Errorf("response error: %w", err)
	}

	data, _ := io.ReadAll(resp.Body)
	duration, _ := strconv.Atoi(string(data))
	log.Info("Recibió respuesta, durmiendo...", "timer", duration)

	sleepDone := make(chan struct{})
	go func() {
		time.Sleep(time.Duration(duration) * time.Millisecond)
		sleepDone <- struct{}{}
		fmt.Println("sleep goroutine closed")
	}()
	defer close(sleepDone)

	select {
	case <-sleepDone:
		return true, nil
	case <-ctx.Done():
		return false, nil
	}
}

func interrupt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Interruptions not implemented

		data, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Instance.Error("Error reading request body", "error", err)
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		message := string(data)
		logger.Instance.Info("Received message from kernel", "message", message)

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Message received."))
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
