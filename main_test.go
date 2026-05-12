//go:build integration

package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/sjr/dnshealth_exporter/cache"
	"github.com/sjr/dnshealth_exporter/config"
	"github.com/sjr/dnshealth_exporter/prober"
)

func TestApplyReloadedConfig_ClearsAddressOverrides(t *testing.T) {
	// Reload regression: if an operator removes all address_overrides
	// and reloads, the prober must stop using the old override mapping.
	// Pre-fix, the assignment was gated on len(overrides) > 0 — so an
	// empty new config silently kept the old behavior.

	// Restore prober.ResolveAddress after the test so other tests in
	// the package aren't affected.
	saved := prober.ResolveAddress
	defer func() { prober.ResolveAddress = saved }()

	// Fixture Setup — initial config with an override active
	oldCfg := &config.Config{
		Zones:            []string{"example.test"},
		AddressOverrides: map[string]string{"1.2.3.4": "10.0.0.1:9999"},
	}
	var current atomic.Pointer[config.Config]
	delegationCache := cache.NewDelegationCache(30 * time.Minute)
	applyReloadedConfig(oldCfg, &current, delegationCache)

	if got := prober.ResolveAddress("1.2.3.4"); got != "10.0.0.1:9999" {
		t.Fatalf("setup: override not active: got %q, want %q", got, "10.0.0.1:9999")
	}

	// Exercise SUT — operator removes all overrides and reloads
	newCfg := &config.Config{Zones: []string{"example.test"}}
	applyReloadedConfig(newCfg, &current, delegationCache)

	// Verification — override is gone; default behavior restored
	if got := prober.ResolveAddress("1.2.3.4"); got != "1.2.3.4:53" {
		t.Errorf("after reload removing overrides: got %q, want default %q",
			got, "1.2.3.4:53")
	}

	// And the new config is stored
	if current.Load() != newCfg {
		t.Errorf("currentConfig not swapped to newCfg")
	}
}

func TestApplyReloadedConfig_AppliesNewOverrides(t *testing.T) {
	// Reload regression: starting with no overrides, adding some via
	// reload should activate them.
	saved := prober.ResolveAddress
	defer func() { prober.ResolveAddress = saved }()

	// Fixture Setup — initial config without overrides
	oldCfg := &config.Config{Zones: []string{"example.test"}}
	var current atomic.Pointer[config.Config]
	delegationCache := cache.NewDelegationCache(30 * time.Minute)
	applyReloadedConfig(oldCfg, &current, delegationCache)

	if got := prober.ResolveAddress("1.2.3.4"); got != "1.2.3.4:53" {
		t.Fatalf("setup: expected default behavior, got %q", got)
	}

	// Exercise SUT — operator adds an override and reloads
	newCfg := &config.Config{
		Zones:            []string{"example.test"},
		AddressOverrides: map[string]string{"1.2.3.4": "10.0.0.1:9999"},
	}
	applyReloadedConfig(newCfg, &current, delegationCache)

	// Verification — override is now active
	if got := prober.ResolveAddress("1.2.3.4"); got != "10.0.0.1:9999" {
		t.Errorf("after reload adding overrides: got %q, want %q",
			got, "10.0.0.1:9999")
	}
}

func TestReload_AddsNewZone(t *testing.T) {
	t.Skip("TODO: requires subprocess testing — start binary, write new config, send SIGHUP, verify new zone in metrics")
}

func TestReload_RemovesZone(t *testing.T) {
	t.Skip("TODO: requires subprocess testing — start with A+B, reload with only A, verify B absent")
}

func TestReload_InvalidConfigKeepsOld(t *testing.T) {
	t.Skip("TODO: requires subprocess testing — start with A, write invalid config, send SIGHUP, verify A still works")
}

func TestReload_InvalidatesDelegationCache(t *testing.T) {
	t.Skip("TODO: requires subprocess testing — verify delegation cache is cleared on reload")
}

func Test503BeforeFirstCycle(t *testing.T) {
	t.Skip("TODO: requires subprocess testing — start binary, immediately GET /metrics, assert 503")
}
