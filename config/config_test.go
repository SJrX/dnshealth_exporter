package config

import (
	"os"
	"path/filepath"
	"testing"
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
