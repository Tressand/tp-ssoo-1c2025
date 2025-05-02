package configManager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func LoadConfig[ConfigObject interface{}](filepath string, v *ConfigObject) error {
	jsonFile, err := os.Open(filepath)
	if err != nil {
		return err
	}
	defer jsonFile.Close()

	jsonParser := json.NewDecoder(jsonFile)
	jsonParser.DisallowUnknownFields()
	jsonParser.UseNumber()
	err = jsonParser.Decode(&v)
	if err != nil {
		return err
	}

	return nil
}

func SaveConfig[ConfigObject struct{}](filepath string, v *ConfigObject) error {
	output, err := json.Marshal(*v)
	if err != nil {
		return err
	}

	jsonFile, err := os.OpenFile(filepath, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0666)
	if err != nil {
		return err
	}
	defer jsonFile.Close()
	_, err = jsonFile.Write(output)
	if err != nil {
		return err
	}

	return nil
}

// Magic to figure out if the program is running on "go run" or an executable
func IsCompiledEnv() bool {
	exePath, err := os.Executable()
	if err != nil {
		panic(err)
	}
	return !(strings.Contains(exePath, os.TempDir()) || strings.Contains(exePath, ".cache"))
}

func GetDefaultExePath() string {
	exePath, err := os.Executable()
	if err != nil {
		panic(err)
	}

	if IsCompiledEnv() {
		exePath = filepath.Dir(exePath)
	} else {
		_, modulePath, _, ok := runtime.Caller(1)
		if !ok {
			panic("runtime.Caller failed")
		}
		exePath, _ = strings.CutSuffix(modulePath, "/config/config.go")
	}

	return exePath
}
