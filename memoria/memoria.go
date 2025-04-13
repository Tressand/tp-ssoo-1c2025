package main

import (
	"fmt"
	"net/http"
	"os"
	"ssoo-memoria/config"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/menu"
	"ssoo-utils/parsers"
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
	mux.Handle("/storage", storageRequestHandler())

	// Sending anything to this channel will shutdown the server.
	// The server will respond back on this same channel to confirm closing.
	shutdownSignal := make(chan any)
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortMemory, mux, shutdownSignal)

	// #endregion

	// #region MENU

	menu := menu.Create()
	menu.Add("Save value on memory", func() {
		var key string
		var value string

		fmt.Print("Enter key: ")
		fmt.Scanln(&key)
		fmt.Print("Enter value (string): ")
		fmt.Scanln(&value)

		saveValue(key, value)
	})
	menu.Add("Read value from memory", func() {
		var key string

		fmt.Print("Enter key: ")
		fmt.Scanln(&key)

		fmt.Println(retrieveValue(key))
	})
	menu.Add("List all values stored", func() {
		for key, value := range storage {
			fmt.Printf("%s | %v\n", key, value)
		}
	})
	menu.Add("Delete value from memory", func() {
		var key string

		fmt.Print("Enter key: ")
		fmt.Scanln(&key)

		deleteValue(key)
	})
	menu.Add("Close Server and Exit Program", func() {
		shutdownSignal <- struct{}{}
		<-shutdownSignal
		close(shutdownSignal)
		os.Exit(0)
	})
	for {
		menu.Activate()
	}

	// #endregion

}

func storageRequestHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params := r.URL.Query()
		if !params.Has("key") || (r.Method == "POST" && !params.Has("value")) {
			w.WriteHeader(http.StatusBadRequest)
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

var storage map[string]any = make(map[string]any)

func saveValue(key string, value any) {
	storage[key] = value
}

func retrieveValue(key string) any {
	return storage[key]
}

func deleteValue(key string) {
	delete(storage, key)
}
