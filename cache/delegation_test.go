package cache

import (
	"testing"
	"time"
)

func TestDelegationCache_SetAndGet(t *testing.T) {
	// Fixture Setup
	c := NewDelegationCache(30 * time.Minute)

	// Exercise SUT
	c.Set("example.com.", "delegation-data")
	result := c.Get("example.com.")

	// Verification
	if result != "delegation-data" {
		t.Errorf("got %v, want delegation-data", result)
	}
}

func TestDelegationCache_MissOnEmpty(t *testing.T) {
	// Fixture Setup
	c := NewDelegationCache(30 * time.Minute)

	// Exercise SUT
	result := c.Get("nonexistent.com.")

	// Verification
	if result != nil {
		t.Errorf("expected nil for cache miss, got %v", result)
	}
}

func TestDelegationCache_ExpiresAfterTTL(t *testing.T) {
	// Fixture Setup — very short TTL
	c := NewDelegationCache(10 * time.Millisecond)
	c.Set("example.com.", "old-data")

	// Wait for TTL to expire
	time.Sleep(20 * time.Millisecond)

	// Exercise SUT
	result := c.Get("example.com.")

	// Verification
	if result != nil {
		t.Errorf("expected nil after TTL expiry, got %v", result)
	}
}

func TestDelegationCache_NotExpiredWithinTTL(t *testing.T) {
	// Fixture Setup
	c := NewDelegationCache(1 * time.Hour)
	c.Set("example.com.", "fresh-data")

	// Exercise SUT
	result := c.Get("example.com.")

	// Verification
	if result != "fresh-data" {
		t.Errorf("got %v, want fresh-data", result)
	}
}

func TestDelegationCache_Invalidate(t *testing.T) {
	// Fixture Setup
	c := NewDelegationCache(1 * time.Hour)
	c.Set("a.com.", "data-a")
	c.Set("b.com.", "data-b")

	// Exercise SUT
	c.Invalidate()

	// Verification
	if c.Get("a.com.") != nil {
		t.Error("expected nil for a.com after invalidate")
	}
	if c.Get("b.com.") != nil {
		t.Error("expected nil for b.com after invalidate")
	}
	if c.Len() != 0 {
		t.Errorf("expected 0 entries after invalidate, got %d", c.Len())
	}
}
