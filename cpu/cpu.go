package main

import (
	"fmt"
	"ssoo-cpu/config"
	"ssoo-utils/parsers"
)

func main() {
	config.Load()
	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))
}
