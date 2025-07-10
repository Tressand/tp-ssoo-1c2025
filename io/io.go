package main

// #region SECTION: IMPORTS

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"ssoo-io/config"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/menu"
	"ssoo-utils/parsers"
	"strconv"
	"strings"
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

	var names []string
	var count = -1
	if len(os.Args) > 1 {
		names = append(names, os.Args[1:]...)
		count = len(names)
	}

	var nstr string
	for count < 0 {
		fmt.Print("How many IO's will we open at start? ")
		fmt.Scanln(&nstr)
		count, err = strconv.Atoi(nstr)
		if err != nil || count < 0 {
			continue
		}
		for n := range count {
			names = append(names, "IO"+fmt.Sprint(n+1))
		}
	}

	var wg sync.WaitGroup
	ctx, cancelctx := context.WithCancel(context.Background())
	for n := range count {
		wg.Add(1)
		go createKernelConnection(names[n], 3, 5, &wg, ctx)
	}
	time.Sleep(5 * time.Millisecond)

	// #endregion

	if !config.Values.ShowMenu {
		wg.Wait()
		return
	}

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
	menu.Add("Close all connections and exit.", func() {
		cancelctx()
		wg.Wait()
		os.Exit(0)
	})
	for {
		menu.Activate()
	}
	// #endregion
}

func createKernelConnection(name string, retryAmount int, retrySeconds int, wg *sync.WaitGroup, ctx context.Context) {
	var assignedPID uint = 0
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			retry, err := notifyKernel(name, &assignedPID)
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

func notifyKernel(name string, pidptr *uint) (bool, error) {
	log := slog.With("name", name)
	log.Info("Notificando a Kernel...")
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "io-notify",
		Queries:  map[string]string{"name": name, "pid": fmt.Sprint(*pidptr)},
	})
	resp, err := http.Post(url, http.MethodPost, http.NoBody)
	if err != nil {
		fmt.Println("Probably the server is not running, logging error")
		log.Error("Error making POST request", "error", err)
		return true, err
	}
	defer resp.Body.Close()

	*pidptr = 0

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTeapot {
			log.Info("Server asked for shutdown.")
			return false, nil
		}
		log.Error("Error on response", "Status", resp.StatusCode, "error", err)
		return true, fmt.Errorf("response error: %w", err)
	}

	data, _ := io.ReadAll(resp.Body)
	vars := strings.Split(string(data), "|")
	pid, _ := strconv.Atoi(vars[0])
	duration, _ := strconv.Atoi(vars[1])

	*pidptr = uint(pid)
	logger.RequiredLog(true, *pidptr, "Inicio de IO", map[string]string{"Tiempo": fmt.Sprint(duration) + "ms"})
	time.Sleep(time.Duration(duration) * time.Millisecond)
	logger.RequiredLog(true, *pidptr, "Fin de IO", map[string]string{})

	notifyIOFinished(pid)

	return true, nil
}

func notifyIOFinished(pid int) {
	slog.Info("Notificando a Kernel que IO ha finalizado...")
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "io-finished",
		Queries:  map[string]string{"pid": strconv.Itoa(pid)},
	})
	resp, err := http.Post(url, http.MethodPost, http.NoBody)
	if err != nil {
		slog.Error("Error making POST request", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Error on response", "Status", resp.StatusCode, "error", err)
		return
	}

	slog.Info("IO finalizado notificado correctamente")
}
