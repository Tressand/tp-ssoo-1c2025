package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"ssoo-utils/configManager"
	"sync"
	"time"
)

type Module struct {
	name            string
	sourceDir       string
	outputFile      string
	configFile      string
	startTimestamp  time.Time
	finishTimestamp time.Time
}

// To build:
// cd (this directory)
// go build -o ../../build_all.exe build_all.go

var scriptDir string
var buildsDir string
var configsDir string

func main() {
	// Get the directory of the executable
	var exePath string
	var err error
	if configManager.IsCompiledEnv() {
		exePath, err = os.Executable()
		if err != nil {
			fmt.Println("Error getting executable path:", err)
			return
		}
		scriptDir = filepath.Dir(exePath)
	} else {
		scriptDir, err = filepath.Abs("./")
		if err != nil {
			fmt.Println("Error getting executable path:", err)
			return
		}
	}

	// Define directories
	buildsDir = filepath.Join(scriptDir, "builds")
	configsDir = filepath.Join(buildsDir, "config")

	// Create directories
	if err := os.MkdirAll(buildsDir, 0755); err != nil {
		fmt.Println("Error creating builds directory:", err)
		return
	}
	if err := os.MkdirAll(configsDir, 0755); err != nil {
		fmt.Println("Error creating configs directory:", err)
		return
	}

	// Define modules and their configurations
	modules := []Module{
		{"kernel", "ssoo-kernel", "kernel.exe", "kernel/config/kernel_config.json", time.Now(), time.Now()},
		{"cpu", "ssoo-cpu", "cpu.exe", "cpu/config/cpu_config.json", time.Now(), time.Now()},
		{"io", "ssoo-io", "io.exe", "io/config/io_config.json", time.Now(), time.Now()},
		{"memoria", "ssoo-memoria", "memoria.exe", "memoria/config/memoria_config.json", time.Now(), time.Now()},
	}
	initialTimestamp := time.Now()

	if runtime.NumCPU() > 1 {
		fmt.Println("Detected multiple CPU's, building async.")
		wg := new(sync.WaitGroup)
		for index := range modules {
			wg.Add(1)
			go func() {
				defer wg.Done()
				buildModule(&modules[index])
			}()
		}
		wg.Wait()
	} else {
		fmt.Println("Detected a single CPU, building sequentially.")
		for index := range modules {
			buildModule(&modules[index])
		}
	}

	fmt.Println("Build process completed:")
	for _, module := range modules {
		fmt.Printf("  %s's build process took %dms.\n", module.name, module.finishTimestamp.Sub(module.startTimestamp).Milliseconds())
	}
	fmt.Printf("Total build time: %dms.\n", time.Since(initialTimestamp).Milliseconds())
}

func buildModule(module *Module) {
	module.startTimestamp = time.Now()

	outputPath := filepath.Join(buildsDir, module.outputFile)
	configSource := filepath.Join(scriptDir, module.configFile)
	configDest := filepath.Join(configsDir, filepath.Base(module.configFile))

	// Build the module
	fmt.Printf("Building %s...\n", module.name)
	cmd := exec.Command("go", "build", "-o", outputPath, module.sourceDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error building %s: %v\n", module.name, err)
		return
	}

	// Copy the configuration file
	if err := copyFile(configSource, configDest); err != nil {
		fmt.Printf("Error copying config for %s: %v\n", module.name, err)
	}

	module.finishTimestamp = time.Now()
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read source file: %w", err)
	}

	if err := os.WriteFile(dst, input, 0644); err != nil {
		return fmt.Errorf("failed to write destination file: %w", err)
	}

	return nil
}
