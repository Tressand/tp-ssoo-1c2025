package main

import (
	"fmt"
	"net/http"
	"ssoo-cpu/config"
	"ssoo-utils/parsers"
)

func main() {
	config.Load()
	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))

	key, value := getInput()

	url := buildUrl(config.Values.MemoryURL, key, value)

	fmt.Printf("Connecting to %s\n", url)
	resp, err := http.Post(url, http.MethodPost, http.NoBody)

	fmt.Println(parsers.Struct(resp))
	fmt.Println(err)

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

func buildUrl(baseURL, key, value string) string {

	return fmt.Sprintf("%s/storage?key=%s&value=%s", baseURL, key, value)
}
