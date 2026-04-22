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
	// Point nameserver discovery at our test root fixture.
	prober.DefaultResolver = "127.240.0.1:" + testutil.TestPort

	// All test servers run on TestPort, so override address resolution.
	prober.ResolveAddress = func(ip string) string {
		return net.JoinHostPort(ip, testutil.TestPort)
	}

	os.Exit(m.Run())
}
