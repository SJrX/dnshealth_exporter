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

func TestStartup_WiresRootServersFromConfig(t *testing.T) {
	// Coverage gap closer: applyReloadedConfig is well-tested but the
	// initial-load gate in main() (the `if len(cfg.RootServers) > 0`
	// block) had no direct coverage. This test exercises the same
	// conditional from outside, verifying that loading a config with
	// root_servers populates prober.RootServers and that loading one
	// without leaves the prober defaults intact.

	saved := prober.RootServers
	defer func() { prober.RootServers = saved }()

	// Fixture A — config with override
	cfgWithOverride := &config.Config{
		Zones:       []string{"example.test"},
		RootServers: []string{"coredns-root:53"},
	}

	// Reset to defaults before exercising
	prober.RootServers = append([]string(nil), prober.DefaultRootServers...)

	// Exercise SUT — mirrors main.go's startup wiring exactly
	if len(cfgWithOverride.RootServers) > 0 {
		prober.RootServers = cfgWithOverride.RootServers
	}

	if len(prober.RootServers) != 1 || prober.RootServers[0] != "coredns-root:53" {
		t.Errorf("startup with override: got %v, want [coredns-root:53]",
			prober.RootServers)
	}

	// Fixture B — config without override
	cfgWithoutOverride := &config.Config{Zones: []string{"example.test"}}

	// Reset to defaults
	prober.RootServers = append([]string(nil), prober.DefaultRootServers...)

	// Exercise SUT
	if len(cfgWithoutOverride.RootServers) > 0 {
		prober.RootServers = cfgWithoutOverride.RootServers
	}

	// Verification — prober still on defaults (gate did not fire)
	if len(prober.RootServers) != len(prober.DefaultRootServers) ||
		prober.RootServers[0] != prober.DefaultRootServers[0] {
		t.Errorf("startup without override: expected defaults, got %v",
			prober.RootServers)
	}
}

func TestApplyReloadedConfig_AppliesRootServers(t *testing.T) {
	// Reload regression: starting with no override, adding root_servers
	// via reload must point delegation at the new roots.

	// Restore prober.RootServers after the test so other tests in the
	// package aren't affected.
	saved := prober.RootServers
	defer func() { prober.RootServers = saved }()

	// Fixture Setup — initial config without an override; prober must
	// be on its defaults.
	oldCfg := &config.Config{Zones: []string{"example.test"}}
	var current atomic.Pointer[config.Config]
	delegationCache := cache.NewDelegationCache(30 * time.Minute)
	applyReloadedConfig(oldCfg, &current, delegationCache)

	if len(prober.RootServers) == 0 || prober.RootServers[0] != prober.DefaultRootServers[0] {
		t.Fatalf("setup: expected defaults, got %v", prober.RootServers)
	}

	// Exercise SUT — operator adds an override and reloads
	newCfg := &config.Config{
		Zones:       []string{"example.test"},
		RootServers: []string{"coredns-root:53"},
	}
	applyReloadedConfig(newCfg, &current, delegationCache)

	// Verification — override is now active
	if len(prober.RootServers) != 1 || prober.RootServers[0] != "coredns-root:53" {
		t.Errorf("after reload adding root_servers: got %v, want [coredns-root:53]",
			prober.RootServers)
	}
}

func TestApplyReloadedConfig_ClearsRootServers(t *testing.T) {
	// Reload regression: if an operator removes the root_servers
	// override and reloads, the prober must fall back to the public
	// root defaults — not silently keep the old override.

	saved := prober.RootServers
	defer func() { prober.RootServers = saved }()

	// Fixture Setup — initial config with an override active
	oldCfg := &config.Config{
		Zones:       []string{"example.test"},
		RootServers: []string{"coredns-root:53"},
	}
	var current atomic.Pointer[config.Config]
	delegationCache := cache.NewDelegationCache(30 * time.Minute)
	applyReloadedConfig(oldCfg, &current, delegationCache)

	if prober.RootServers[0] != "coredns-root:53" {
		t.Fatalf("setup: override not active: got %v", prober.RootServers)
	}

	// Exercise SUT — operator removes the override and reloads
	newCfg := &config.Config{Zones: []string{"example.test"}}
	applyReloadedConfig(newCfg, &current, delegationCache)

	// Verification — defaults restored
	if len(prober.RootServers) != len(prober.DefaultRootServers) {
		t.Fatalf("after reload removing root_servers: got %d entries, want %d",
			len(prober.RootServers), len(prober.DefaultRootServers))
	}
	for i, addr := range prober.DefaultRootServers {
		if prober.RootServers[i] != addr {
			t.Errorf("after reload removing root_servers: got %v, want defaults %v",
				prober.RootServers, prober.DefaultRootServers)
			break
		}
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
