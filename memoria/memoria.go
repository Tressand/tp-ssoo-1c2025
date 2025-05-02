package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
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
	storageMenu := menu.Create()
	storageMenu.Add("Save value on memory", func() {
		var key string
		var value string

		fmt.Print("Enter key: ")
		fmt.Scanln(&key)
		fmt.Print("Enter value (string): ")
		fmt.Scanln(&value)

		saveValue(key, value)
	})
	storageMenu.Add("Read value from memory", func() {
		var key string

		fmt.Print("Enter key: ")
		fmt.Scanln(&key)

		fmt.Println(retrieveValue(key))
	})
	storageMenu.Add("List all values stored", func() {
		for key, value := range testStorage {
			fmt.Printf("%s | %v\n", key, value)
		}
	})
	storageMenu.Add("Delete value from memory", func() {
		var key string

		fmt.Print("Enter key: ")
		fmt.Scanln(&key)

		deleteValue(key)
	})
	mainMenu.Add("Access Storage", func() {
		storageMenu.Activate()
	})
	mainMenu.Add("Create Process", func() {
		codeFolder := config.Values.CodeFolder
		files, _ := os.ReadDir(codeFolder)
		var names []string
		for _, file := range files {
			names = append(names, file.Name())
		}
		i := menu.SliceSelect(names)
		err := storage.CreateProcess(1, codeFolder+"/"+files[i].Name(), 0)
		if err != nil {
			slog.Error("process could not be created", "error", err)
		} else {
			slog.Info("process created")
		}
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
			if !params.Has("code") || !params.Has("req") {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("missing codefile or memory requirement"))
				return
			}
			req, _ := strconv.Atoi(params.Get("req"))
			codepath := config.Values.CodeFolder + "/" + params.Get("code")
			err := storage.CreateProcess(pid, codepath, req)
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
