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
	"strings"
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

	// #endregion

	// #region CREATE SERVER

	// Create mux
	var mux *http.ServeMux = http.NewServeMux()

	// Add routes to mux
	mux.Handle("/system_memory", systemMemoryReqHandler())
	// mux.Handle("/user_memory", userMemoryReqHandler())
	// mux.Handle("/memory_dump", memoryDumpReqHandler())
	// mux.Handle("/full_page", fullPageReqHandler())
	// mux.Handle("/memory_config", memoryConfigReqHandler())
	mux.Handle("/free_space", freeSpaceRequestHandler())

	// Sending anything to this channel will shutdown the server.
	// The server will respond back on this same channel to confirm closing.
	shutdownSignal := make(chan any)
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortMemory, mux, shutdownSignal)

	// #endregion

	// #region MENU

	var _internalProcessCounter uint = 1

	mainMenu := menu.Create()

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
	mainMenu.Add("Test Logic Address", func() {
	START:
		for {
			fmt.Print("Address: ")
			var input string
			fmt.Scanln(&input)
			sliceAddress := strings.Split(input, "|")
			address := make([]int, len(sliceAddress))
			for i, char := range sliceAddress {
				num, err := strconv.Atoi(char)
				if err != nil {
					fmt.Println("Invalid Address.")
					goto START
				}
				address[i] = num
			}
			fmt.Println("Final Address: ", address)
			fmt.Println(storage.GetLogicAddress(3, address))
		}
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

	// #endregion

}

type MethodRequestInfo struct {
	ReqParams []string
	Callback  func(w http.ResponseWriter, r *http.Request) SimpleResponse
}

type SimpleResponse struct {
	Status int
	Body   []byte
}

func (response SimpleResponse) send(w http.ResponseWriter) {
	w.WriteHeader(response.Status)
	_, err := w.Write(response.Body)
	if err != nil {
		slog.Error("error sending response", "status", response.Status, "body", response.Body)
	}
}

type RequestOptions map[string]MethodRequestInfo

func genericRequestHandler(w http.ResponseWriter, r *http.Request, options RequestOptions) {
	defer r.Body.Close()
	fmt.Println("Request: ", r.Method, ":/", r.URL.String())
	requestInfo, ok := options[r.Method]
	if !ok {
		SimpleResponse{http.StatusMethodNotAllowed, []byte("method " + r.Method + " not allowed")}.send(w)
		return
	}
	params := r.URL.Query()
	for _, query := range requestInfo.ReqParams {
		if !params.Has(query) {
			SimpleResponse{http.StatusBadRequest, []byte("missing " + query + " query param")}.send(w)
			return
		}
	}
	requestInfo.Callback(w, r).send(w)
}

func numFromQuery(r *http.Request, key string) int {
	val, _ := strconv.Atoi(r.URL.Query().Get(key))
	return val
}

// esta API de procesos acepta GET, POST y DELETE.
// todas las peticiones devuelven 502:BadGateway si tuvo un error interno y 400:BadRequest si la petición está mal.
// los errores se pueden obtener del body de la response.
// GET:

// 	recibe 2 querys: pid y pc.
// 	devuelve 200:OK y la instrucción en json si la encontró

// POST:

// 	recibe 2 querys: pid y size.
//  recibe el código por body de request.
// 	devuelve 200:OK si creó el proceso

// DELETE:

// recibe 1 query: pid
// devuelve 200:OK si encontró el proceso y lo borra.

func systemMemoryReqHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		get := MethodRequestInfo{
			ReqParams: []string{"pid", "pc"},
			Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
				instruction, err := storage.GetInstruction(
					uint(numFromQuery(r, "pid")),
					numFromQuery(r, "pc"),
				)
				if err != nil {
					return SimpleResponse{http.StatusBadGateway, []byte(err.Error())}
				}
				body, err := json.Marshal(instruction)
				if err != nil {
					return SimpleResponse{http.StatusBadGateway, []byte(err.Error())}
				}
				return SimpleResponse{200, body}
			},
		}
		post := MethodRequestInfo{
			ReqParams: []string{"pid", "size"},
			Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
				err := storage.CreateProcess(
					uint(numFromQuery(r, "pid")),
					r.Body,
					numFromQuery(r, "size"),
				)
				if err != nil {
					return SimpleResponse{http.StatusBadGateway, []byte(err.Error())}
				}
				return SimpleResponse{200, []byte{}}
			},
		}
		delete := MethodRequestInfo{
			ReqParams: []string{"pid"},
			Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
				err := storage.DeleteProcess(uint(numFromQuery(r, "pid")))
				if err != nil {
					return SimpleResponse{http.StatusBadGateway, []byte(err.Error())}
				}
				return SimpleResponse{200, []byte{}}
			},
		}

		options := RequestOptions{
			"GET":    get,
			"POST":   post,
			"DELETE": delete,
		}

		genericRequestHandler(w, r, options)
	}
}

func freeSpaceRequestHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Add("contentType", "text/plain")
		w.Write([]byte(fmt.Sprint(storage.GetRemainingMemory())))
	}
}
