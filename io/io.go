package main

import (
	"fmt"
	"ssoo-io/config"
	"ssoo-utils/parsers"
)

func main() {
	config.Load()
	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Config))
}
