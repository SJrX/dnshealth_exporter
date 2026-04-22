//go:build integration

package prober_test

import (
	"net"
	"os"
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	"github.com/sjr/dnshealth_exporter/testutil"
)

func TestMain(m *testing.M) {
	// Override root servers to point at our test root fixture.
	// This is the only override — the delegation walker, nameserver
	// discovery, and probers all follow the same code path as
	// production.
	prober.RootServers = []string{"127.240.0.1:" + testutil.TestPort}

	// All test servers run on TestPort.
	prober.ResolveAddress = func(ip string) string {
		return net.JoinHostPort(ip, testutil.TestPort)
	}

	os.Exit(m.Run())
}
