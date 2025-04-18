package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"ssoo-cpu/config"
	"ssoo-utils/httputils"
	"ssoo-utils/parsers"
)

func main() {
	config.Load()
	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))

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
