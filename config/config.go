package config

import (
	"fmt"
	"net"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Config is the top-level configuration for the exporter.
type Config struct {
	Zones []string `yaml:"zones"`

	// AddressOverrides maps an IP address to a host:port pair.
	// When the exporter discovers a nameserver at a given IP,
	// it queries the overridden address instead.
	//
	// This is useful for testing (nameservers on non-standard
	// ports) and production scenarios like querying through
	// proxies or non-standard port deployments.
	//
	// Example:
	//   address_overrides:
	//     "127.240.0.2": "127.240.0.2:10053"
	//     "10.0.0.5": "10.0.0.5:5353"
	AddressOverrides map[string]string `yaml:"address_overrides"`
}

// ResolveAddress returns the address to query for a given
// nameserver IP. If an override exists, it's used; otherwise
// the IP is returned with the default DNS port (53).
func (c *Config) ResolveAddress(ip string) string {
	if c.AddressOverrides != nil {
		if override, ok := c.AddressOverrides[ip]; ok {
			return override
		}
	}
	return net.JoinHostPort(ip, "53")
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
