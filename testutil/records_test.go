package testutil

import (
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
	if soa.Serial == 0 {
		t.Error("default serial should be non-zero")
	}
	if soa.Hdr.Name != "example.test." {
		t.Errorf("name: got %s, want example.test.", soa.Hdr.Name)
	}
	if soa.Refresh != 3600 {
		t.Errorf("default refresh: got %d, want 3600", soa.Refresh)
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

func TestAAAA_CreatesValidRecord(t *testing.T) {
	// Exercise SUT
	rr := AAAA("ns1.example.test", "2001:db8::2")

	// Verification
	a, ok := rr.(*dns.AAAA)
	if !ok {
		t.Fatal("expected *dns.AAAA")
	}
	if a.AAAA.String() != "2001:db8::2" {
		t.Errorf("ip: got %s, want 2001:db8::2", a.AAAA.String())
	}
	if a.Hdr.Rrtype != dns.TypeAAAA {
		t.Errorf("rrtype: got %d, want %d (TypeAAAA)", a.Hdr.Rrtype, dns.TypeAAAA)
	}
}
