package testutil

import (
	"net"
	"sync/atomic"

	"github.com/miekg/dns"
)

// serialCounter provides unique SOA serials across tests.
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

// Mname sets the SOA MNAME field (the primary master nameserver).
// miekg/dns calls this field `Ns` on the SOA struct, which is easy
// to misread — this helper makes test intent obvious.
func Mname(name string) SOAOption {
	return func(soa *dns.SOA) { soa.Ns = dns.Fqdn(name) }
}

// SOA creates a dns.SOA record with sensible defaults.
// Only specify the options that matter for your test.
// The serial auto-increments by default. Use Serial(n) to set
// a specific value.
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

// AAAA creates a dns.AAAA record. Mirrors A() for IPv6 — same
// signature shape, same defaults, accepts any textual IPv6 form
// that net.ParseIP understands.
func AAAA(name, ipv6 string) dns.RR {
	return &dns.AAAA{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(name),
			Rrtype: dns.TypeAAAA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		AAAA: net.ParseIP(ipv6),
	}
}

// CNAME creates a dns.CNAME record aliasing name to target.
func CNAME(name, target string) dns.RR {
	return &dns.CNAME{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(name),
			Rrtype: dns.TypeCNAME,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Target: dns.Fqdn(target),
	}
}

// TXT creates a dns.TXT record at name. Pass one string for the common
// case, or several to model a record split into multiple character-
// strings (RFC 7208 §3.3 / RFC 7489 §A.5) that a parser must concatenate.
// Used for SPF (at the zone apex) and DMARC (at `_dmarc.<zone>`).
func TXT(name string, strings ...string) dns.RR {
	return &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(name),
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Txt: strings,
	}
}

// MX creates a dns.MX record for a zone. Preference is the priority
// (lower = preferred per RFC 5321 §5.1). For Null MX (RFC 7505) use
// preference 0 and exchange ".".
func MX(zone string, preference uint16, exchange string) dns.RR {
	return &dns.MX{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(zone),
			Rrtype: dns.TypeMX,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Preference: preference,
		Mx:         dns.Fqdn(exchange),
	}
}
