package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"ssoo-cpu/config"
	"ssoo-utils/httputils"
	"ssoo-utils/parsers"
	"strconv"
)

func main() {
	config.Load()
	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))

	// ID CPU -> Kernel

	if len(os.Args) < 2 {
		slog.Error("No CPU ID provided")
		return
	}

	cpuId := os.Args[1]

	//

	key, value := getInput()

	url := httputils.BuildUrl(httputils.URLData{
		Base:     config.Values.IpMemory,
		Endpoint: "storage",
		Queries: map[string]string{
			"key":   key,
			"value": value,
		},
	})

	fmt.Printf("Connecting to %s\n", url)
	resp, err := http.Post(url, http.MethodPost, http.NoBody)
	if err != nil {
		slog.Error("POST to Memory failed", "Error", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("POST to Memory status wrong", "status", resp.StatusCode)
		return
	}

	slog.Info("POST to Memory succeded")

	cpuIp := httputils.GetOutboundIP()
	cpuPort := strconv.Itoa(config.Values.PortCPU)

	urlKernel := httputils.BuildUrl(httputils.URLData{
		Base:     config.Values.IpKernel,
		Endpoint: "cpu-handshake",
		Queries: map[string]string{
			"ip":   cpuIp,
			"port": cpuPort,
			"id":   cpuId,
		},
	})
	fmt.Printf("Connecting to %s\n", urlKernel)

	res, err := http.Post(urlKernel, http.MethodPost, http.NoBody)
	if err != nil {
		slog.Error("POST to Kernel failed", "Error", err)
	}
	res.Body.Close()

	if res.StatusCode != http.StatusOK {
		slog.Error("POST to Kernel status wrong", "status", res.StatusCode)
		return
	}

	slog.Info("POST to Kernel succeded")

}

func getInput() (string, string) {
	fmt.Print("Key: ")
	var key string
	var value string
	fmt.Scanln(&key)
	fmt.Print("Value: ")
	fmt.Scanln(&value)

	return key, value
}
