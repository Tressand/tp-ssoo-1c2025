package configManager

import (
	"encoding/json"
	"os"
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
