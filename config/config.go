package config

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Config is the top-level configuration for the exporter.
type Config struct {
	Zones []string `yaml:"zones"`

	// ProbeInterval is how often probe cycles run. Default: 60s.
	ProbeInterval time.Duration `yaml:"probe_interval"`

	// DelegationCacheTTL is how long delegation walk results are cached.
	// Only applies to non-target infrastructure (root, TLD, parent).
	// Default: 30m.
	DelegationCacheTTL time.Duration `yaml:"delegation_cache_ttl"`

	// QueryTimeout is the timeout for individual DNS queries. Default: 5s.
	QueryTimeout time.Duration `yaml:"query_timeout"`

	// ZoneDeadline is the overall deadline per zone. Outstanding queries
	// are cancelled when this expires. Default: 30s.
	ZoneDeadline time.Duration `yaml:"zone_deadline"`

	// AddressOverrides maps an IP address to a host:port pair.
	AddressOverrides map[string]string `yaml:"address_overrides"`

	// RootServers, if non-empty, replaces the prober's hardcoded list of
	// public root DNS server addresses for delegation walking. Used by
	// the demo deployment to point delegation walks at an in-stack fake
	// root so the demo runs offline. When empty, the prober's defaults
	// are used. Each entry MUST be host:port form (e.g. "127.0.0.1:53").
	RootServers []string `yaml:"root_servers"`
}

// ResolveAddress returns the address to query for a given
// nameserver IP. If an override exists, it's used; otherwise
// the IP is returned with the default DNS port (53).
//
// The lookup key is canonicalised via net.ParseIP(ip).String() so
// that operator-supplied keys in the override map (canonicalised
// at load time by canonicaliseOverrideKeys below) match runtime
// IPs regardless of textual form — e.g. "2001:DB8::1" matches
// "2001:db8::1" matches "2001:0db8:0000:0000:0000:0000:0000:0001".
// All three canonicalise to the same RFC 5952 short form.
//
// If the input is not a parseable IP (shouldn't happen at runtime
// — probers always pass valid IPs from miekg/dns), fall through to
// net.JoinHostPort with the original string and let the downstream
// query surface the error.
func (c *Config) ResolveAddress(ip string) string {
	if c.AddressOverrides != nil {
		key := ip
		if parsed := net.ParseIP(ip); parsed != nil {
			key = parsed.String()
		}
		if override, ok := c.AddressOverrides[key]; ok {
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

	cfg.applyDefaults()

	if err := cfg.canonicaliseOverrideKeys(); err != nil {
		return nil, err
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// canonicaliseOverrideKeys rewrites every key in AddressOverrides
// to its canonical RFC 5952 / IPv4-dotted form via net.ParseIP. Any
// key that does not parse as an IP is rejected with a clear error
// (today such keys would silently never match anything). Per spec
// 006 FR-013.
func (c *Config) canonicaliseOverrideKeys() error {
	if len(c.AddressOverrides) == 0 {
		return nil
	}
	canonical := make(map[string]string, len(c.AddressOverrides))
	for k, v := range c.AddressOverrides {
		parsed := net.ParseIP(k)
		if parsed == nil {
			return fmt.Errorf("config: address_overrides key %q is not a valid IP address", k)
		}
		key := parsed.String()
		if existing, dup := canonical[key]; dup && existing != v {
			return fmt.Errorf("config: address_overrides key %q collides with another entry "+
				"after canonicalisation (both canonicalise to %q with different values)", k, key)
		}
		canonical[key] = v
	}
	c.AddressOverrides = canonical
	return nil
}

func (c *Config) applyDefaults() {
	if c.ProbeInterval == 0 {
		c.ProbeInterval = 60 * time.Second
	}
	if c.DelegationCacheTTL == 0 {
		c.DelegationCacheTTL = 30 * time.Minute
	}
	if c.QueryTimeout == 0 {
		c.QueryTimeout = 5 * time.Second
	}
	if c.ZoneDeadline == 0 {
		c.ZoneDeadline = 30 * time.Second
	}
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
