package httputils

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

func StartHTTPServer(ip string, port int, mux *http.ServeMux, shutdownSignal chan interface{}) {
	// Create server with config and mux
	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", ip, port),
		Handler: mux,
	}
	// Send goroutine to listen and serve
	var waitserverstart = make(chan struct{})
	go func() {
		slog.Info("Arranca servidor en: " + server.Addr)
		waitserverstart <- struct{}{}
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("Error arrancando servidor HTTP: %v", "error", err)
		}
		slog.Info("Servidor HTTP Detenido")
	}()
	// Wait for shutdown signal
	go func() {
		<-shutdownSignal
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			fmt.Println()
		}
		shutdownSignal <- struct{}{}
	}()
	// Wait for server to start
	<-waitserverstart
}

func GetOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		slog.Error("Error obteniendo IP local: %v", "error", err)
		return ""
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

type URLData struct {
	Ip       string
	Port     int
	Endpoint string
	Queries  map[string]string
}

func BuildUrl(data URLData) string {
	url := "http://" + data.Ip + ":" + fmt.Sprint(data.Port) + "/" + data.Endpoint
	if len(data.Queries) > 0 {
		url += "?"
		for key, value := range data.Queries {
			url += key + "=" + value + "&"
		}
		url, _ = strings.CutSuffix(url, "&")
	}
	return url
}
