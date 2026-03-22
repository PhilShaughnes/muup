package main

import (
	"os"

	"github.com/BurntSushi/toml"
)

// Config represents the application configuration
type Config struct {
	Monitors []MonitorConfig `toml:"monitor"`
}

// MonitorConfig represents a monitor from config file
type MonitorConfig struct {
	Name       string `toml:"name"`
	URL        string `toml:"url"`
	Interval   int    `toml:"interval"`
	Timeout    int    `toml:"timeout"`
	Expected   int    `toml:"expected"`
	SkipVerify bool   `toml:"skip_verify"`
}

// LoadConfig reads and parses the TOML config file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, err
	}

	// Set defaults
	for i := range cfg.Monitors {
		if cfg.Monitors[i].Interval == 0 {
			cfg.Monitors[i].Interval = 30
		}
		if cfg.Monitors[i].Timeout == 0 {
			cfg.Monitors[i].Timeout = 5000
		}
		if cfg.Monitors[i].Expected == 0 {
			cfg.Monitors[i].Expected = 200
		}
	}

	return &cfg, nil
}
