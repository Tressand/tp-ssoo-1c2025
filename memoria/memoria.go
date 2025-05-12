package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"ssoo-memoria/config"
	"ssoo-memoria/storage"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/menu"
	"ssoo-utils/parsers"
	"strconv"
)

func main() {
	// #region SETUP

	config.Load()
	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))
	err := logger.SetupDefault("memoria", config.Values.LogLevel)
	defer logger.Close()
	if err != nil {
		fmt.Printf("Error setting up logger: %v\n", err)
		return
	}
	log := logger.Instance
	log.Info("Arranca Memoria")
	storage.InitializeUserMemory(config.Values.MemorySize)

	// #endregion

	// #region CREATE SERVER

	// Create mux
	var mux *http.ServeMux = http.NewServeMux()

	// Add routes to mux
	mux.Handle("/storage", testStorageRequestHandler())
	mux.Handle("/process", processRequestHandler())
	mux.Handle("/free_space", freeSpaceRequestHandler())
	// Sending anything to this channel will shutdown the server.
	// The server will respond back on this same channel to confirm closing.
	shutdownSignal := make(chan any)
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortMemory, mux, shutdownSignal)

	// #endregion

	// #region MENU

	mainMenu := menu.Create()

	var _internalProcessCounter uint
	mainMenu.Add("Create Process", func() {
		codeFolder, _ := filepath.Abs("./code")

		files, _ := os.ReadDir(codeFolder)
		var names []string
		for _, file := range files {
			names = append(names, file.Name())
		}
		i := menu.SliceSelect(names)

		codeFile, err := os.OpenFile(codeFolder+"/"+files[i].Name(), os.O_RDONLY, 0666)
		if err != nil {
			slog.Error("error opening file")
			return
		}
		defer codeFile.Close()

		fmt.Print("Process size: ")
		var input string
		fmt.Scanln(&input)

		size, err := strconv.Atoi(input)
		if err != nil {
			slog.Error("size not a number")
		}

		err = storage.CreateProcess(_internalProcessCounter, codeFile, size)
		if err != nil {
			slog.Error("process could not be created", "error", err)
			return
		}
		slog.Info("process created", "name", _internalProcessCounter, "size", size)
		_internalProcessCounter++
	})
	mainMenu.Add("Kill process", func() {
		processes := storage.GetProcesses()
		if len(processes) == 0 {
			return
		}
		i := menu.SliceSelect(processes)
		storage.DeleteProcess(processes[i].PID)
	})
	mainMenu.Add("Log processes", storage.LogSystemMemory)
	mainMenu.Add("Close Server and Exit Program", func() {
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

func freeSpaceRequestHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Add("contentType", "text/plain")
		w.Write([]byte(fmt.Sprint(storage.GetRemainingMemory())))
	}
}

// esta API de procesos acepta GET, POST y DELETE.
// todas las peticiones devuelven 502:BadGateway si tuvo un error interno y 400:BadRequest si la petición está mal.
// los errores se pueden obtener del body de la response.
// GET:

// 	recibe 2 querys: pid y pc.
// 	devuelve 200:OK y la instrucción en json si la encontró

// POST:

// 	recibe 2 querys: pid y nombre del archivo de código, la detección de la dirección de dicho archivo es automática.
// 	devuelve 200:OK si creó el proceso

// DELETE:

// recibe 1 query: pid
// devuelve 200:OK si encontró el proceso y lo borra.
func processRequestHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Request: ", r.Method, ":/", r.RequestURI)
		params := r.URL.Query()
		if !params.Has("pid") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("missing pid"))
			return
		}
		conv, _ := strconv.ParseUint(params.Get("pid"), 10, 0)
		pid := uint(conv)

		switch r.Method {
		case "GET":
			if !params.Has("pc") {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("missing pc"))
				return
			}
			pc, _ := strconv.Atoi(params.Get("pc"))
			instruction, err := storage.GetInstruction(pid, pc)
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				w.Write([]byte(err.Error()))
				return
			}
			body, err := json.Marshal(instruction)
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				w.Write([]byte(err.Error()))
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write(body)

		case "POST":
			if !params.Has("req") {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("missing memory requirement"))
				return
			}
			req, _ := strconv.Atoi(params.Get("req"))
			err := storage.CreateProcess(pid, r.Body, req)
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				w.Write([]byte(err.Error()))
				return
			}
			w.WriteHeader(http.StatusOK)

		case "DELETE":
			err := storage.DeleteProcess(pid)
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				w.Write([]byte(err.Error()))
				return
			}
			w.WriteHeader(http.StatusOK)
		}
	}
}

func testStorageRequestHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Request: ", r.Method, ":/", r.RequestURI)
		params := r.URL.Query()
		if !params.Has("key") || (r.Method == "POST" && !params.Has("value")) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Header().Add("contentType", "text/plain")
		key := params.Get("key")
		switch r.Method {
		case "GET":
			w.Write([]byte(fmt.Sprint(retrieveValue(key))))
		case "POST":
			saveValue(key, params.Get("value"))
		case "DELETE":
			deleteValue(key)
		}
	}
}

var testStorage map[string]string = make(map[string]string)

func saveValue(key string, value string) {
	testStorage[key] = value
}

func retrieveValue(key string) string {
	return testStorage[key]
}

func deleteValue(key string) {
	delete(testStorage, key)
}
