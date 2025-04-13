package main

// #region SECTION: IMPORTS

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"ssoo-io/config"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/parsers"
	"strconv"
	"sync"
	"time"
)

// #endregion

var urlKernel string = ""

func main() {
	// #region SETUP

	config.Load()
	if config.Values.IpKernel == "self" {
		config.Values.IpKernel = httputils.GetOutboundIP()
	}
	urlKernel = "http://" + config.Values.IpKernel + ":" + fmt.Sprint(config.Values.PortKernel)
	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))
	err := logger.SetupDefault("io", config.Values.LogLevel)
	defer logger.Close()
	if err != nil {
		fmt.Printf("Error setting up logger: %v\n", err)
		return
	}
	slog.Info("Arranca IO")

	// #endregion

	const startingCount = 5
	var wg sync.WaitGroup
	for n := range startingCount {
		wg.Add(1)
		go createKernelConnection("IO"+fmt.Sprint(n+1), 3, 5, &wg)
	}
	wg.Wait()
}

func createKernelConnection(name string, retryAmount int, retrySeconds int, wg *sync.WaitGroup) {
	for {
		retry, err := notifyKernel(name)
		if !retry {
			break
		}
		if err != nil {
			if retryAmount <= 0 {
				break
			}
			time.Sleep(time.Duration(retrySeconds) * time.Second)
			retryAmount--
		}
	}
	wg.Done()
}

func notifyKernel(name string) (bool, error) {
	log := slog.With("name", name)
	log.Info("Notificando a Kernel...")
	resp, err := http.Post(urlKernel+"/io-notify", "text/plain", bytes.NewBufferString(name))
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

	time.Sleep(time.Duration(duration) * time.Millisecond)

	log.Info("Terminó de dormir")

	return true, nil
}
