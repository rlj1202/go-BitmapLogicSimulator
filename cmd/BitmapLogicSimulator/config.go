package main

import (
	"encoding/json"
	"os"
)

type Config struct {
	FileName            string
	SimulationsPerFrame int
}

func loadConfig(configFileName string) (*Config, error) {
	configFile, err := os.Open(configFileName)
	defer configFile.Close()
	if err != nil {
		return &Config{"", 5}, nil
	}

	decoder := json.NewDecoder(configFile)

	c := new(Config)

	err = decoder.Decode(&c)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func saveConfig(configFileName string, c *Config) error {
	configFile, err := os.Create(configFileName)
	defer configFile.Close()
	if err != nil {
		return err
	}

	bytes, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		return nil
	}

	_, err = configFile.Write(bytes)
	if err != nil {
		return err
	}

	return nil
}
