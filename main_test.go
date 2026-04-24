//go:build integration

package main

import "testing"

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
