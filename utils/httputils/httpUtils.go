package httputils

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"ssoo-utils/logger"
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
		logger.Instance.Info("Starting HTTP server at " + server.Addr)
		waitserverstart <- struct{}{}
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Instance.Error("Error starting HTTP server: %v", "error", err)
		}
		logger.Instance.Info("HTTP server stopped")
	}()
	// Wait for shutdown signal
	go func() {
		<-shutdownSignal
		shutdownCtx := context.Background()
		shutdownCtx, cancel := context.WithTimeout(shutdownCtx, 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Instance.Error("error shutting down http server", "error", err)
		}
		shutdownSignal <- struct{}{}
	}()
	// Wait for server to start
	<-waitserverstart
}

func GetOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		logger.Instance.Error("Error getting outbound IP: %v", "error", err)
		return ""
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
