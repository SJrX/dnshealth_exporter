package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_ValidConfig(t *testing.T) {
	// Fixture Setup
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte("zones:\n  - example.com\n  - example.org\n"), 0644)

	// Exercise SUT
	cfg, err := Load(path)

	// Verification
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Zones) != 2 {
		t.Fatalf("expected 2 zones, got %d", len(cfg.Zones))
	}
	if cfg.Zones[0] != "example.com" {
		t.Errorf("expected first zone example.com, got %s", cfg.Zones[0])
	}
}

func TestLoad_EmptyZones(t *testing.T) {
	// Fixture Setup
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte("zones: []\n"), 0644)

	// Exercise SUT
	_, err := Load(path)

	// Verification
	if err == nil {
		t.Fatal("expected error for empty zones, got nil")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	// Exercise SUT
	_, err := Load("/nonexistent/config.yml")

	// Verification
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	// Fixture Setup
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte("{{{{not yaml"), 0644)

	// Exercise SUT
	_, err := Load(path)

	// Verification
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoad_DefaultTimingValues(t *testing.T) {
	// Fixture Setup — no timing fields specified
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte("zones:\n  - example.com\n"), 0644)

	// Exercise SUT
	cfg, err := Load(path)

	// Verification — defaults applied
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ProbeInterval != 60*time.Second {
		t.Errorf("probe_interval: got %v, want 60s", cfg.ProbeInterval)
	}
	if cfg.DelegationCacheTTL != 30*time.Minute {
		t.Errorf("delegation_cache_ttl: got %v, want 30m", cfg.DelegationCacheTTL)
	}
	if cfg.QueryTimeout != 5*time.Second {
		t.Errorf("query_timeout: got %v, want 5s", cfg.QueryTimeout)
	}
	if cfg.ZoneDeadline != 30*time.Second {
		t.Errorf("zone_deadline: got %v, want 30s", cfg.ZoneDeadline)
	}
}

func TestLoad_CustomTimingValues(t *testing.T) {
	// Fixture Setup
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`zones:
  - example.com
probe_interval: 120s
delegation_cache_ttl: 1h
query_timeout: 10s
zone_deadline: 45s
`), 0644)

	// Exercise SUT
	cfg, err := Load(path)

	// Verification
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ProbeInterval != 120*time.Second {
		t.Errorf("probe_interval: got %v, want 120s", cfg.ProbeInterval)
	}
	if cfg.DelegationCacheTTL != time.Hour {
		t.Errorf("delegation_cache_ttl: got %v, want 1h", cfg.DelegationCacheTTL)
	}
	if cfg.QueryTimeout != 10*time.Second {
		t.Errorf("query_timeout: got %v, want 10s", cfg.QueryTimeout)
	}
	if cfg.ZoneDeadline != 45*time.Second {
		t.Errorf("zone_deadline: got %v, want 45s", cfg.ZoneDeadline)
	}
}

func TestLoad_RootServersOverride(t *testing.T) {
	// Fixture Setup
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`zones:
  - example.com
root_servers:
  - coredns-root:53
  - 127.0.0.1:5353
`), 0644)

	// Exercise SUT
	cfg, err := Load(path)

	// Verification
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.RootServers) != 2 {
		t.Fatalf("expected 2 root servers, got %d", len(cfg.RootServers))
	}
	if cfg.RootServers[0] != "coredns-root:53" || cfg.RootServers[1] != "127.0.0.1:5353" {
		t.Errorf("root_servers: got %v, want [coredns-root:53 127.0.0.1:5353]", cfg.RootServers)
	}
}

func TestLoad_RootServersDefaultsEmpty(t *testing.T) {
	// Fixture Setup — no root_servers field; field MUST be empty (not
	// populated with defaults) so the prober keeps its own defaults.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte("zones:\n  - example.com\n"), 0644)

	// Exercise SUT
	cfg, err := Load(path)

	// Verification
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.RootServers) != 0 {
		t.Errorf("expected empty root_servers when omitted, got %v", cfg.RootServers)
	}
}

func TestLoad_InvalidDomainName(t *testing.T) {
	// Fixture Setup
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte("zones:\n  - notadomain\n"), 0644)

	// Exercise SUT
	_, err := Load(path)

	// Verification
	if err == nil {
		t.Fatal("expected error for invalid domain name, got nil")
	}
}
