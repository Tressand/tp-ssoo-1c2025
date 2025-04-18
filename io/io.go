package main

// #region SECTION: IMPORTS

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"ssoo-io/config"
	"ssoo-utils/logger"
	"ssoo-utils/menu"
	"ssoo-utils/parsers"
	"strconv"
	"sync"
	"time"
)

// #endregion

func main() {
	// #region SETUP

	config.Load()
	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))
	err := logger.SetupDefault("io", config.Values.LogLevel)
	defer logger.Close()
	if err != nil {
		fmt.Printf("Error setting up logger: %v\n", err)
		return
	}
	slog.Info("Arranca IO")

	// #endregion

	// #region INITIAL THREADS

	var nstr string
	var count = -1
	for count < 0 {
		fmt.Print("How many IO's will we open at start? ")
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
		go createKernelConnection("IO"+fmt.Sprint(n+1), 3, 5, &wg, ctx)
	}
	time.Sleep(5 * time.Millisecond)

	// #endregion

	// #region MENU

	menu := menu.Create()
	menu.Add("Add new IO thread.", func() {
		wg.Add(1)
		count++
		go createKernelConnection("IO"+fmt.Sprint(count), 3, 5, &wg, ctx)
	})
	menu.Add("Wait for all IO's to close and exit.", func() {
		wg.Wait()
		os.Exit(0)
	})
	menu.Add("Forcefully close all IO's and exit.", func() {
		cancelctx()
		os.Exit(0)
	})
	for {
		menu.Activate()
	}
	// #endregion
}

func createKernelConnection(name string, retryAmount int, retrySeconds int, wg *sync.WaitGroup, ctx context.Context) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			retry, err := notifyKernel(name)
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

func notifyKernel(name string) (bool, error) {
	log := slog.With("name", name)
	log.Info("Notificando a Kernel...")
	resp, err := http.Post(config.Values.KernelURL+"/io-notify", "text/plain", bytes.NewBufferString(name))
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
