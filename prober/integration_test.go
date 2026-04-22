//go:build integration

package prober_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/sjr/dnshealth_exporter/testutil"
)

func TestMain(m *testing.M) {
	// Find project root (where testdata/ lives)
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v\n", err)
		os.Exit(1)
	}
	// Walk up to find project root
	for {
		if _, err := os.Stat(dir + "/testdata/docker-compose.yml"); err == nil {
			break
		}
		parent := dir[:max(0, len(dir)-1)]
		for parent != "" && parent[len(parent)-1] != '/' {
			parent = parent[:len(parent)-1]
		}
		if parent == "" || parent == dir {
			fmt.Fprintf(os.Stderr, "could not find testdata/docker-compose.yml\n")
			os.Exit(1)
		}
		dir = parent[:len(parent)-1]
	}

	if err := testutil.StartDockerCompose(dir); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start docker compose: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	if err := testutil.StopDockerCompose(dir); err != nil {
		fmt.Fprintf(os.Stderr, "failed to stop docker compose: %v\n", err)
	}

	os.Exit(code)
}
