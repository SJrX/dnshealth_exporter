package testutil

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sjr/dnshealth_exporter/prober"
)

// ContainerIPs maps container names to their loopback IPs.
var ContainerIPs = map[string]string{
	"root": "127.240.0.1",
	"ns1":  "127.240.0.2",
	"ns2":  "127.240.0.3",
	"ns3":  "127.240.0.4",
}

// DNSFixture manages CoreDNS test fixtures.
type DNSFixture struct {
	t       testing.TB
	baseDir string
}

// NewDNSFixture creates a fixture manager. The baseDir should be
// the testdata/coredns/runtime directory.
func NewDNSFixture(t testing.TB) *DNSFixture {
	baseDir := findRuntimeDir(t)
	return &DNSFixture{t: t, baseDir: baseDir}
}

// WriteZone replaces the entire zone directory for a container
// with the provided zone file content. Each test fully declares
// what each nameserver serves.
func (f *DNSFixture) WriteZone(container, content string) *DNSFixture {
	f.t.Helper()
	dir := filepath.Join(f.baseDir, container, "zones")
	// Clear existing zone files
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() == ".gitkeep" {
			continue
		}
		os.Remove(filepath.Join(dir, e.Name()))
	}
	// Write new zone file
	zone := extractZoneName(content)
	path := filepath.Join(dir, zone+".zone")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		f.t.Fatalf("WriteZone(%s): %v", container, err)
	}
	return f
}

// Reload waits for CoreDNS to pick up zone file changes.
// CoreDNS is configured with reload 1s, so we wait 2s to be safe.
func (f *DNSFixture) Reload(t testing.TB) *DNSFixture {
	t.Helper()
	time.Sleep(2 * time.Second)
	return f
}

// Probe calls a prober function against a zone and returns the
// registry containing the registered metrics.
func (f *DNSFixture) Probe(fn prober.ProbeFn, zone string) *prometheus.Registry {
	f.t.Helper()
	registry := prometheus.NewRegistry()
	client := &dns.Client{Timeout: 5 * time.Second}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := context.Background()

	if err := fn(ctx, zone, client, registry, logger); err != nil {
		f.t.Logf("Probe returned error: %v", err)
	}
	return registry
}

// RunProber calls a named prober (with common metrics) and returns the registry.
func (f *DNSFixture) RunProber(name, zone string) *prometheus.Registry {
	f.t.Helper()
	registry := prometheus.NewRegistry()
	client := &dns.Client{Timeout: 5 * time.Second}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := context.Background()

	prober.RunProber(ctx, name, zone, client, registry, logger)
	return registry
}

func findRuntimeDir(t testing.TB) string {
	t.Helper()
	// Walk up from the test's working directory to find testdata
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "testdata", "coredns", "runtime")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find testdata/coredns/runtime from %s", dir)
		}
		dir = parent
	}
}

func extractZoneName(content string) string {
	// Extract zone name from $ORIGIN line or first record
	for _, line := range splitLines(content) {
		if len(line) > 8 && line[:8] == "$ORIGIN " {
			name := line[8:]
			// Remove trailing dot
			if len(name) > 0 && name[len(name)-1] == '.' {
				name = name[:len(name)-1]
			}
			return name
		}
	}
	return "zone"
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// StartDockerCompose starts the Docker Compose fixtures.
// Call this from TestMain.
func StartDockerCompose(dir string) error {
	cmd := exec.Command("docker", "compose", "-f",
		filepath.Join(dir, "testdata", "docker-compose.yml"),
		"up", "-d")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	// Wait for CoreDNS to be ready
	time.Sleep(3 * time.Second)
	return nil
}

// StopDockerCompose stops the Docker Compose fixtures.
func StopDockerCompose(dir string) error {
	cmd := exec.Command("docker", "compose", "-f",
		filepath.Join(dir, "testdata", "docker-compose.yml"),
		"down")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
