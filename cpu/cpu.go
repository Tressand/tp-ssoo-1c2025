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

	// #region INITIAL THREADS

	var nstr string
	var count = -1
	for count < 0 {
		fmt.Print("How many CPU's will we open at start? ")
		fmt.Scanln(&nstr)
		count, err = strconv.Atoi(nstr)
		if err != nil || count < 0 {
			continue
		}
	}

	var wg sync.WaitGroup
	ctx, cancelctx := context.WithCancel(context.Background())
	for n := range count {
		wg.Add(1)
		go createKernelConnection("CPU"+fmt.Sprint(n+1), 3, 5, &wg, ctx)
	}
	time.Sleep(5 * time.Millisecond)

	// #endregion

	var mux *http.ServeMux = http.NewServeMux()

	mux.Handle("/message-from-kernel", messageFromKernel())

	shutdownSignal := make(chan any)
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortCPU, mux, shutdownSignal)
	/*
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
	*/
	mainMenu := menu.Create()
	moduleMenu := menu.Create()
	moduleMenu.Add("Add new CPU thread.", func() {
		wg.Add(1)
		count++
		go createKernelConnection("CPU"+fmt.Sprint(count), 3, 5, &wg, ctx)
	})
	moduleMenu.Add("Wait for all CPU's to close and exit.", func() {
		wg.Wait()
		os.Exit(0)
	})
	moduleMenu.Add("Forcefully close all CPU's and exit.", func() {
		cancelctx()
		os.Exit(0)
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

}

func createKernelConnection(id string, retryAmount int, retrySeconds int, wg *sync.WaitGroup, ctx context.Context) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			retry, err := notifyKernel(id)
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

/*
func notifyKernel(id string) (bool, error) {
	log := slog.With("cpu_id", id)
	log.Info("Notificando a Kernel...")

	ip := httputils.GetOutboundIP()
	port := strconv.Itoa(config.Values.PortCPU)

	url := httputils.BuildUrl(httputils.URLData{
		Base:     config.Values.IpKernel,
		Endpoint: "cpu-notify",
		Queries: map[string]string{
			"ip":   ip,
			"port": port,
			"id":   id,
		},
	})

	resp, err := http.Post(url, "text/plain", http.NoBody)

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
	valor, _ := strconv.Atoi(string(data))
	log.Info("Recibió respuesta, valor", "timer", valor)

	return true, nil
} */

func notifyKernel(id string) (bool, error) {
	log := slog.With("cpu_id", id)
	log.Info("Notificando a Kernel...")

	ip := httputils.GetOutboundIP()
	port := strconv.Itoa(config.Values.PortCPU)

	url := httputils.BuildUrl(httputils.URLData{
		Base:     config.Values.IpKernel,
		Endpoint: "cpu-notify",
		Queries: map[string]string{
			"ip":   ip,
			"port": port,
			"id":   id,
		},
	})
	log.Info("Connecting to Kernel", "url", url)

	resp, err := http.Get(url)

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
	value, _ := strconv.Atoi(string(data))
	log.Info("Recibió respuesta, value: ", "timer", value)

	return true, nil
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
