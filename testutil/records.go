package testutil

import (
	"fmt"
	"net"
	"strings"
	"sync/atomic"

	"github.com/miekg/dns"
)

// serialCounter provides monotonically increasing SOA serials
// so that CoreDNS's file plugin always accepts zone reloads.
var serialCounter atomic.Uint32

func init() {
	serialCounter.Store(1000000)
}

func nextSerial() uint32 {
	return serialCounter.Add(1)
}

// SOAOption configures a field on a SOA record.
type SOAOption func(*dns.SOA)

// Serial sets the SOA serial number.
func Serial(n uint32) SOAOption {
	return func(soa *dns.SOA) { soa.Serial = n }
}

// Refresh sets the SOA refresh interval.
func Refresh(n uint32) SOAOption {
	return func(soa *dns.SOA) { soa.Refresh = n }
}

// Retry sets the SOA retry interval.
func Retry(n uint32) SOAOption {
	return func(soa *dns.SOA) { soa.Retry = n }
}

// Expire sets the SOA expire interval.
func Expire(n uint32) SOAOption {
	return func(soa *dns.SOA) { soa.Expire = n }
}

// Minttl sets the SOA minimum TTL.
func Minttl(n uint32) SOAOption {
	return func(soa *dns.SOA) { soa.Minttl = n }
}

// SOA creates a dns.SOA record with sensible defaults.
// Only specify the options that matter for your test.
//
// IMPORTANT: The serial is auto-incremented by default so that
// CoreDNS's file plugin always accepts zone reloads (it compares
// serials). Use Serial(n) to set a specific value — but be aware
// that the ZoneFile wrapper will inject a higher "reload serial"
// into a separate SOA if needed to force CoreDNS to reload.
func SOA(zone string, opts ...SOAOption) dns.RR {
	zone = dns.Fqdn(zone)
	soa := &dns.SOA{
		Hdr: dns.RR_Header{
			Name:   zone,
			Rrtype: dns.TypeSOA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Ns:      "ns1." + zone,
		Mbox:    "hostmaster." + zone,
		Serial:  nextSerial(),
		Refresh: 3600,
		Retry:   300,
		Expire:  2419200,
		Minttl:  300,
	}
	for _, opt := range opts {
		opt(soa)
	}
	return soa
}

// NS creates a dns.NS record for the given zone and nameserver.
func NS(zone, nameserver string) dns.RR {
	return &dns.NS{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(zone),
			Rrtype: dns.TypeNS,
			Class:  dns.ClassINET,
			Ttl:    172800,
		},
		Ns: dns.Fqdn(nameserver),
	}
}

// A creates a dns.A record.
func A(name, ip string) dns.RR {
	return &dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(name),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: net.ParseIP(ip),
	}
}

// ZoneFile serializes DNS records into zone file text suitable for
// CoreDNS's file plugin. The zone parameter sets the $ORIGIN.
func ZoneFile(zone string, records ...dns.RR) string {
	zone = dns.Fqdn(zone)
	var b strings.Builder
	fmt.Fprintf(&b, "$ORIGIN %s\n", zone)
	for _, rr := range records {
		b.WriteString(rr.String())
		b.WriteByte('\n')
	}
	return b.String()
}
