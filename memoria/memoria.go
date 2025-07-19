package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"ssoo-memoria/config"
	"ssoo-memoria/storage"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/parsers"
	"strconv"
	"time"
)

// Sending anything to this channel will shutdown the server.
// The server will respond back on this same channel to confirm closing.
var shutdownSignal chan any = make(chan any)

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
	mux.Handle("/process", processDataReqHandler.HandlerFunc())
	mux.Handle("/frame", processFrameReqHandler.HandlerFunc())
	mux.Handle("/user_memory", userMemoryReqHandler.HandlerFunc())
	mux.Handle("/memory_dump", memoryDumpReqHandler.HandlerFunc())
	mux.Handle("/full_page", fullPageReqHandler.HandlerFunc())
	mux.Handle("/memory_config", memoryConfigReqHandler.HandlerFunc())
	mux.Handle("/suspend", suspendProcessRequestHandler.HandlerFunc())
	mux.Handle("/unsuspend", unsuspendProcessRequestHandler.HandlerFunc())
	mux.Handle("/free_space", freeSpaceRequestHandler.HandlerFunc())
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		go func() {
			fmt.Println("Se solició cierre. o7")
			shutdownSignal <- struct{}{}
			<-shutdownSignal
			os.Exit(0)
		}()
	})
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortMemory, mux, shutdownSignal)

	// #endregion

	storage.InitializeUserMemory()

	select {}
}

// #region GENERIC REQUEST API REST HANDLER

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

type GenericRequest map[string]MethodRequestInfo

func (request GenericRequest) HandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		requestInfo, ok := request[r.Method]
		if !ok {
			requestInfo, ok = request["ANY"]
			if !ok {
				SimpleResponse{http.StatusMethodNotAllowed, []byte("method " + r.Method + " not allowed")}.send(w)
				return
			}
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
}

func numFromQuery(r *http.Request, key string) int {
	val, _ := strconv.Atoi(r.URL.Query().Get(key))
	return val
}

// #endregion

// #region APIS

var processDataReqHandler = GenericRequest{
	"GET": MethodRequestInfo{
		ReqParams: []string{"pid", "pc"},
		Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
			time.Sleep(time.Duration(config.Values.MemoryDelay) * time.Millisecond)
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
			return SimpleResponse{http.StatusOK, body}
		},
	},
	"POST": MethodRequestInfo{
		ReqParams: []string{"pid", "size"},
		Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
			time.Sleep(time.Duration(config.Values.MemoryDelay) * time.Millisecond)
			err := storage.CreateProcess(
				uint(numFromQuery(r, "pid")),
				r.Body,
				numFromQuery(r, "size"),
			)
			if err != nil {
				return SimpleResponse{http.StatusBadGateway, []byte(err.Error())}
			}
			return SimpleResponse{http.StatusOK, []byte{}}
		},
	},
	"DELETE": MethodRequestInfo{
		ReqParams: []string{"pid"},
		Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
			time.Sleep(time.Duration(config.Values.MemoryDelay) * time.Millisecond)
			err := storage.DeleteProcess(uint(numFromQuery(r, "pid")))
			if err != nil {
				return SimpleResponse{http.StatusBadGateway, []byte(err.Error())}
			}
			return SimpleResponse{http.StatusOK, []byte{}}
		},
	},
}

var processFrameReqHandler = GenericRequest{
	"GET": MethodRequestInfo{
		ReqParams: []string{"pid", "address"},
		Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
			fmt.Printf("CPU solicita frame (PID: %v, DL: %v)\n", r.URL.Query()["pid"], r.URL.Query()["address"])
			frameBase, err := storage.LogicAddressToFrame(
				uint(numFromQuery(r, "pid")),
				storage.StringToLogicAddress(r.URL.Query().Get("address")),
			)
			if err != nil {
				return SimpleResponse{http.StatusBadRequest, []byte(err.Error())}
			}
			return SimpleResponse{http.StatusOK, []byte(fmt.Sprint(frameBase))}
		},
	},
}

var userMemoryReqHandler = GenericRequest{
	"GET": MethodRequestInfo{
		ReqParams: []string{"pid", "base", "delta"},
		Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
			time.Sleep(time.Duration(config.Values.MemoryDelay) * time.Millisecond)
			pid, base, delta := uint(numFromQuery(r, "pid")), numFromQuery(r, "base"), numFromQuery(r, "delta")
			result, err := storage.GetFromMemory(pid, base, delta)
			if err != nil {
				return SimpleResponse{http.StatusBadRequest, []byte(err.Error())}
			}
			logger.RequiredLog(true, pid, "Lectura", map[string]string{
				"Dir.Física": fmt.Sprint(base + delta),
				"Tamaño":     "1",
			})
			return SimpleResponse{http.StatusOK, []byte{result}}
		},
	},
	"POST": MethodRequestInfo{
		ReqParams: []string{"pid", "base", "delta"},
		Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
			time.Sleep(time.Duration(config.Values.MemoryDelay) * time.Millisecond)
			pid, base, delta := uint(numFromQuery(r, "pid")), numFromQuery(r, "base"), numFromQuery(r, "delta")
			value, _ := io.ReadAll(r.Body)
			err := storage.WriteToMemory(pid, base, delta, value[0])
			if err != nil {
				return SimpleResponse{http.StatusBadRequest, []byte(err.Error())}
			}
			logger.RequiredLog(true, pid, "Escritura", map[string]string{
				"Dir.Física": fmt.Sprint(base + delta),
				"Tamaño":     "1",
			})
			return SimpleResponse{http.StatusOK, []byte{}}
		},
	},
}

var memoryDumpReqHandler = GenericRequest{
	"ANY": MethodRequestInfo{
		ReqParams: []string{"pid"},
		Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
			time.Sleep(time.Duration(config.Values.MemoryDelay) * time.Millisecond)
			err := storage.Memory_Dump(uint(numFromQuery(r, "pid")))
			if err != nil {
				return SimpleResponse{http.StatusBadGateway, []byte(err.Error())}
			}
			return SimpleResponse{http.StatusOK, []byte{}}
		},
	},
}

var memoryConfigReqHandler = GenericRequest{
	"ANY": MethodRequestInfo{
		Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
			body, err := json.Marshal(storage.GetConfig())
			if err != nil {
				return SimpleResponse{http.StatusBadGateway, []byte(err.Error())}
			}
			return SimpleResponse{http.StatusOK, body}
		},
	},
}

var fullPageReqHandler = GenericRequest{
	"GET": MethodRequestInfo{
		ReqParams: []string{"pid", "base"},
		Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
			time.Sleep(time.Duration(config.Values.MemoryDelay*config.Values.PageSize) * time.Millisecond)
			pid, base := uint(numFromQuery(r, "pid")), numFromQuery(r, "base")
			if ok, err := storage.HasPage(pid, base); !ok {
				return SimpleResponse{http.StatusBadRequest, []byte(err.Error())}
			}
			page, err := storage.GetPage(pid, base)
			if err != nil {
				return SimpleResponse{http.StatusBadRequest, []byte(err.Error())}
			}
			logger.RequiredLog(true, pid, "Lectura", map[string]string{
				"Dir.Física": fmt.Sprint(base),
				"Tamaño":     fmt.Sprint(storage.GetConfig().PageSize),
			})
			return SimpleResponse{http.StatusOK, page}
		},
	},
	"POST": MethodRequestInfo{
		ReqParams: []string{"pid", "base"},
		Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
			time.Sleep(time.Duration(config.Values.MemoryDelay*config.Values.PageSize) * time.Millisecond)
			pid, base := uint(numFromQuery(r, "pid")), numFromQuery(r, "base")
			value, _ := io.ReadAll(r.Body)
			err := storage.WritePage(pid, base, value)
			if err != nil {
				return SimpleResponse{http.StatusBadRequest, []byte(err.Error())}
			}
			logger.RequiredLog(true, pid, "Escritura", map[string]string{
				"Dir.Física": fmt.Sprint(base),
				"Tamaño":     fmt.Sprint(storage.GetConfig().PageSize),
			})
			return SimpleResponse{http.StatusOK, []byte{}}
		},
	},
}

var freeSpaceRequestHandler = GenericRequest{
	"ANY": MethodRequestInfo{
		Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
			return SimpleResponse{http.StatusOK, []byte(fmt.Sprint(storage.GetRemainingMemory()))}
		},
	},
}

var suspendProcessRequestHandler = GenericRequest{
	"ANY": MethodRequestInfo{
		ReqParams: []string{"pid"},
		Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
			time.Sleep(time.Duration(config.Values.SwapDelay) * time.Millisecond)
			err := storage.SuspendProcess(uint(numFromQuery(r, "pid")))
			if err != nil {
				slog.Error(err.Error())
				return SimpleResponse{http.StatusBadGateway, []byte(err.Error())}
			}
			return SimpleResponse{http.StatusOK, []byte{}}
		},
	},
}

var unsuspendProcessRequestHandler = GenericRequest{
	"ANY": MethodRequestInfo{
		ReqParams: []string{"pid"},
		Callback: func(w http.ResponseWriter, r *http.Request) SimpleResponse {
			time.Sleep(time.Duration(config.Values.SwapDelay) * time.Millisecond)
			err := storage.UnSuspendProcess(uint(numFromQuery(r, "pid")))
			if err != nil {
				slog.Error(err.Error())
				return SimpleResponse{http.StatusBadGateway, []byte(err.Error())}
			}
			return SimpleResponse{http.StatusOK, []byte{}}
		},
	},
}

// #endregion
