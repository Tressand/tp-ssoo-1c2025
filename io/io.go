package main

// #region SECTION: IMPORTS

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"ssoo-io/config"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/parsers"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// #endregion

var instances []struct {
	name   string
	pidptr *uint
}

var port string

var shutdownSignal = make(chan any)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Faltan parámetros. Uso: ./io <identificador> ...[nombre]")
		return
	}
	id := os.Args[1]
	fmt.Println("Identificador recibido: ", id)
	id_int, err := strconv.Atoi(id)
	if err != nil {
		fmt.Printf("Error al convertir el identificador '%s' a entero: %v\n", id, err)
		return
	}

	// #region SETUP

	config.Load()
	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))
	err = logger.SetupDefault("io", config.Values.LogLevel)
	defer logger.Close()
	if err != nil {
		fmt.Printf("Error setting up logger: %v\n", err)
		return
	}
	slog.Info("Arranca IO")
	port = fmt.Sprint(config.Values.PortIO + id_int)

	// #endregion

	var mux *http.ServeMux = http.NewServeMux()
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		go func() {
			fmt.Println("Se solició cierre. o7")
			shutdownSignal <- struct{}{}
			<-shutdownSignal
			os.Exit(0)
		}()
	})

	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortIO+id_int, mux, shutdownSignal)

	// #region INITIAL THREADS

	var names []string
	var count = -1
	if len(os.Args) > 1 {
		names = append(names, os.Args[2:]...)
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
		for n := 0; n < count; n++ {
			names = append(names, "IO"+fmt.Sprint(n+1))
		}
	}
	slog.Info("Starting IO instances", "count", count, "names", names)

	kernelPing := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "/ping",
	})
	_, err = http.Get(kernelPing)
	if err != nil {
		fmt.Println("Esperando a Kernel")
	}
	for err != nil {
		time.Sleep(1 * time.Second)
		_, err = http.Get(kernelPing)
	}

	force_kill_chan := make(chan os.Signal, 1)
	signal.Notify(force_kill_chan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-force_kill_chan
		fmt.Println()
		for _, instance := range instances {
			notifyIODisconnected(instance.name, instance.pidptr)
		}
		shutdownSignal <- struct{}{}
		<-shutdownSignal
		os.Exit(0)
	}()

	var wg sync.WaitGroup
	for n := range count {
		wg.Add(1)
		go createKernelConnection(names[n], &wg)
	}

	// #endregion

	wg.Wait()
}

func createKernelConnection(name string, wg *sync.WaitGroup) {
	defer wg.Done()

	var assignedPID uint = 0

	instances = append(instances, struct {
		name   string
		pidptr *uint
	}{name: name, pidptr: &assignedPID})

	for {
		retry, err := notifyKernel(name, &assignedPID)
		if err != nil {
			slog.Error(err.Error())
		}
		if retry {
			time.Sleep(1 * time.Second)
			continue
		}
		notifyIODisconnected(name, &assignedPID)
		break
	}
}

func notifyKernel(name string, pidptr *uint) (bool, error) {
	log := slog.With("name", name)
	log.Info("Notificando a Kernel...")

	ip := httputils.GetOutboundIP()

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "io-notify",
		Queries: map[string]string{
			"ip":   ip,
			"port": port,
			"name": fmt.Sprint(name), // NO TOCAR NUNCA
			"pid":  fmt.Sprint(*pidptr)},
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
		return false, fmt.Errorf("response error: %w", err)
	}

	data, _ := io.ReadAll(resp.Body)
	vars := strings.Split(string(data), "|")
	pid, _ := strconv.Atoi(vars[0])
	duration, _ := strconv.Atoi(vars[1])

	*pidptr = uint(pid)
	logger.RequiredLog(true, *pidptr, "Inicio de IO", map[string]string{"Tiempo": fmt.Sprint(duration) + "ms"})
	time.Sleep(time.Duration(duration) * time.Millisecond)
	logger.RequiredLog(true, *pidptr, "Fin de IO", map[string]string{})

	notifyIOFinished(name, pid)

	return true, nil
}

func notifyIOFinished(name string, pid int) {
	slog.Info("Notificando a Kernel que IO ha finalizado...")
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "io-finished",
		Queries: map[string]string{
			"ip":   fmt.Sprint(httputils.GetOutboundIP()),
			"port": port,
			"name": name,
			"pid":  fmt.Sprint(pid)},
	})
	req, err := http.NewRequest(http.MethodPost, url, nil) // nil == no body
	if err != nil {
		slog.Error("Error creando request", "error", err)
		return
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Error haciendo POST", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Error en la respuesta", "Status", resp.StatusCode)
		return
	}

	slog.Info("IO finalizado notificado correctamente")
}

func notifyIODisconnected(name string, pidptr *uint) {
	slog.Info("Notificando a Kernel que IO ha sido desconectado...")

	ip := httputils.GetOutboundIP()

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "io-disconnected",
		Queries:  map[string]string{"ip": ip, "port": port, "name": name, "pid": fmt.Sprint(*pidptr)},
	})
	resp, err := http.Post(url, http.MethodPost, http.NoBody)
	if err != nil {
		slog.Info("No se pudo notificar a Kernel de desconección, es posible que se haya desconectado")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Error on response", "Status", resp.StatusCode, "error", err)
		return
	}

	slog.Info("Finalización de IO notificado correctamente")
}
