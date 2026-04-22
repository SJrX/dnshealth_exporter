package testutil

import (
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestSOA_DefaultValues(t *testing.T) {
	// Exercise SUT
	rr := SOA("example.test")

	// Verification
	soa, ok := rr.(*dns.SOA)
	if !ok {
		t.Fatal("expected *dns.SOA")
	}
	if soa.Serial != 2026042101 {
		t.Errorf("default serial: got %d, want 2026042101", soa.Serial)
	}
	if soa.Hdr.Name != "example.test." {
		t.Errorf("name: got %s, want example.test.", soa.Hdr.Name)
	}
}

func TestSOA_WithOverrides(t *testing.T) {
	// Exercise SUT
	rr := SOA("example.test", Serial(42), Refresh(7200))

	// Verification
	soa := rr.(*dns.SOA)
	if soa.Serial != 42 {
		t.Errorf("serial: got %d, want 42", soa.Serial)
	}
	if soa.Refresh != 7200 {
		t.Errorf("refresh: got %d, want 7200", soa.Refresh)
	}
}

func TestNS_CreatesValidRecord(t *testing.T) {
	// Exercise SUT
	rr := NS("example.test", "ns1.example.test")

	// Verification
	ns, ok := rr.(*dns.NS)
	if !ok {
		t.Fatal("expected *dns.NS")
	}
	if ns.Ns != "ns1.example.test." {
		t.Errorf("ns: got %s, want ns1.example.test.", ns.Ns)
	}
}

func TestA_CreatesValidRecord(t *testing.T) {
	// Exercise SUT
	rr := A("ns1.example.test", "127.240.0.2")

	// Verification
	a, ok := rr.(*dns.A)
	if !ok {
		t.Fatal("expected *dns.A")
	}
	if a.A.String() != "127.240.0.2" {
		t.Errorf("ip: got %s, want 127.240.0.2", a.A.String())
	}
}

func TestZoneFile_SerializesRecords(t *testing.T) {
	// Exercise SUT
	content := ZoneFile("example.test",
		SOA("example.test", Serial(1)),
		NS("example.test", "ns1.example.test"),
	)

	// Verification
	if !strings.Contains(content, "$ORIGIN example.test.") {
		t.Error("expected $ORIGIN line")
	}
	if !strings.Contains(content, "SOA") {
		t.Error("expected SOA record")
	}
	if !strings.Contains(content, "ns1.example.test.") {
		t.Error("expected NS record")
	}
}
