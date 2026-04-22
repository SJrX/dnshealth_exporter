package config

import (
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Config is the top-level configuration for the exporter.
type Config struct {
	Zones []string `yaml:"zones"`
}

// Load reads and parses the configuration file at the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if len(c.Zones) == 0 {
		return fmt.Errorf("config: zones list is empty, at least one zone is required")
	}
	for i, z := range c.Zones {
		z = strings.TrimSpace(z)
		if z == "" {
			return fmt.Errorf("config: zone at index %d is empty", i)
		}
		if !strings.Contains(z, ".") {
			return fmt.Errorf("config: zone %q does not look like a valid domain name", z)
		}
		c.Zones[i] = z
	}
	return nil
}
