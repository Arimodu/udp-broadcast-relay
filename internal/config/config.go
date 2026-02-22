package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server ServerConfig `toml:"server"`
	Client ClientConfig `toml:"client"`
}

type ServerConfig struct {
	WebUIPort int    `toml:"webui_port"`
	RelayPort int    `toml:"relay_port"`
	Interface string `toml:"interface"`
	DataDir   string `toml:"data_dir"`
	LogLevel  string `toml:"log_level"`
}

type ClientConfig struct {
	ServerAddress string `toml:"server_address"`
	APIKey        string `toml:"api_key"`
	Interfaces    string `toml:"interfaces"`
	LogLevel      string `toml:"log_level"`
}

func Defaults() *Config {
	return &Config{
		Server: ServerConfig{
			WebUIPort: 21480,
			RelayPort: 14723,
			Interface: "",
			DataDir:   "/var/lib/ubr",
			LogLevel:  "info",
		},
		Client: ClientConfig{
			ServerAddress: "",
			APIKey:        "",
			Interfaces:    "",
			LogLevel:      "info",
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return cfg, nil
}

func Save(path string, cfg *Config) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating config file: %w", err)
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}
