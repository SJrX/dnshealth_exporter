package testutil

import (
	"context"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sjr/dnshealth_exporter/prober"
)

const (
	// TestPort is the fixed port used by all test DNS servers.
	TestPort = "10053"
)

// DNSFixture manages in-process DNS servers for integration tests.
type DNSFixture struct {
	t       testing.TB
	servers []*testServer
}

// ServerOptions configures test server behavior.
type ServerOptions struct {
	// RecursionAvailable makes the server set the RA flag in responses.
	RecursionAvailable bool
	// Rcode overrides the response code (e.g., dns.RcodeNameError for NXDOMAIN).
	Rcode int
	// Drop silently drops all queries (simulates timeout/unreachable).
	Drop bool
	// Garbage sends random bytes instead of a valid DNS response.
	Garbage bool
	// DropFirstN drops the first N queries, then responds normally.
	// Useful for testing retry logic.
	DropFirstN int
}

// QueryCount returns the number of queries received by a server.
// Only works for servers added via ServerWithOptions.
func (f *DNSFixture) QueryCount(addr string) int {
	for _, srv := range f.servers {
		if srv.addr == addr {
			return int(srv.queryCount.Load())
		}
	}
	return 0
}

type testServer struct {
	addr       string
	records    []mdns.RR
	referral   bool // if true, return NS records as referrals (Authority section)
	options    ServerOptions
	server     *mdns.Server
	wg         sync.WaitGroup
	queryCount atomic.Int64
}

// NewDNSFixture creates a new fixture manager.
func NewDNSFixture(t testing.TB) *DNSFixture {
	return &DNSFixture{t: t}
}

// Server adds an authoritative in-process DNS server.
func (f *DNSFixture) Server(addr string, records ...mdns.RR) *DNSFixture {
	f.t.Helper()
	f.servers = append(f.servers, &testServer{
		addr:    addr,
		records: records,
	})
	return f
}

// ServerWithOptions adds an authoritative server with custom behavior.
func (f *DNSFixture) ServerWithOptions(addr string, opts ServerOptions, records ...mdns.RR) *DNSFixture {
	f.t.Helper()
	f.servers = append(f.servers, &testServer{
		addr:    addr,
		records: records,
		options: opts,
	})
	return f
}

// ReferralServer adds a server that returns NS queries as referrals
// (Authority section + glue in Additional), simulating a parent/TLD
// server delegating to authoritative nameservers.
func (f *DNSFixture) ReferralServer(addr string, records ...mdns.RR) *DNSFixture {
	f.t.Helper()
	f.servers = append(f.servers, &testServer{
		addr:     addr,
		records:  records,
		referral: true,
	})
	return f
}

// Start launches all configured DNS servers.
func (f *DNSFixture) Start(t testing.TB) *DNSFixture {
	t.Helper()
	for _, srv := range f.servers {
		startTestServer(t, srv)
	}
	time.Sleep(50 * time.Millisecond)
	return f
}

// Stop shuts down all DNS servers.
func (f *DNSFixture) Stop() {
	for _, srv := range f.servers {
		if srv.server != nil {
			_ = srv.server.ShutdownContext(context.Background())
			srv.wg.Wait()
		}
	}
}

// Probe calls a prober function against a zone, builds a registry
// from the results, and returns it. Performs delegation walk and
// nameserver discovery automatically (like the cycle runner does).
func (f *DNSFixture) Probe(fn prober.ProbeFn, zone string) *prometheus.Registry {
	f.t.Helper()
	client := &mdns.Client{Timeout: 1 * time.Second}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := context.Background()

	// Walk delegation and discover nameservers (same as cycle runner)
	delegation, err := prober.WalkDelegation(ctx, zone, client, logger)
	if err != nil {
		f.t.Logf("Delegation walk error: %v", err)
		return prometheus.NewRegistry()
	}

	// Mirror cycle.runner's discovery flow: seed from parent glue,
	// dedupe, then augment any hostname whose parent glue is missing
	// an IP family by resolving out-of-band. Per spec 006 FR-002 /
	// FR-010 / FR-011.
	seen := make(map[string]struct{})
	var nameservers []prober.Nameserver
	for _, ns := range delegation.NSRecords {
		if ns.IP == "" {
			continue
		}
		key := ns.Hostname + ":" + ns.IP
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		nameservers = append(nameservers, ns)
	}

	// Identify hostnames needing augmentation: no glue at all, or
	// only one IP family present. A hostname with no glue still
	// enters `families` (with both fields false) on its first-seen
	// iteration, so it appears in the keyset below and gets flagged.
	families := make(map[string]struct{ v4, v6 bool })
	for _, ns := range delegation.NSRecords {
		fam := families[ns.Hostname]
		if ns.IP != "" {
			if parsed := net.ParseIP(ns.IP); parsed != nil && parsed.To4() == nil {
				fam.v6 = true
			} else {
				fam.v4 = true
			}
		}
		families[ns.Hostname] = fam
	}
	for host, fam := range families {
		if fam.v4 && fam.v6 {
			continue
		}
		ips, err := prober.ResolveHostnames(ctx, host, client, logger)
		if err != nil {
			f.t.Logf("Resolve hostname error for %s: %v", host, err)
			continue
		}
		for _, ip := range ips {
			key := host + ":" + ip
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			nameservers = append(nameservers, prober.Nameserver{Hostname: host, IP: ip})
		}
	}

	results, err := fn(ctx, zone, nameservers, delegation, client, logger)
	if err != nil {
		f.t.Logf("Probe returned error: %v", err)
	}
	return prober.BuildRegistry(results)
}

func startTestServer(t testing.TB, srv *testServer) {
	t.Helper()

	mux := mdns.NewServeMux()
	mux.HandleFunc(".", func(w mdns.ResponseWriter, r *mdns.Msg) {
		srv.queryCount.Add(1)

		// Drop: silently ignore the query (client will timeout)
		if srv.options.Drop {
			return
		}

		// DropFirstN: drop the first N queries, then respond normally
		if srv.options.DropFirstN > 0 && int(srv.queryCount.Load()) <= srv.options.DropFirstN {
			return
		}

		// Garbage: send random bytes
		if srv.options.Garbage {
			_, _ = w.Write([]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01, 0x02, 0x03})
			return
		}

		m := new(mdns.Msg)
		m.SetReply(r)

		// Override rcode if set
		if srv.options.Rcode != 0 {
			m.Rcode = srv.options.Rcode
			_ = w.WriteMsg(m)
			return
		}

		if len(r.Question) == 0 {
			_ = w.WriteMsg(m)
			return
		}

		qname := r.Question[0].Name
		qtype := r.Question[0].Qtype

		// Collect matching records
		var matched []mdns.RR
		for _, rr := range srv.records {
			if rr.Header().Name != qname {
				continue
			}
			if qtype == mdns.TypeANY || rr.Header().Rrtype == qtype {
				matched = append(matched, rr)
			}
		}

		if srv.referral && len(matched) > 0 && qtype == mdns.TypeNS {
			// Referral mode: NS records go in Authority, glue in Additional.
			// Both A and AAAA records matching an NS hostname are attached
			// as glue — real-world parent referrals may include either or
			// both families; the exporter must consume what's there.
			m.Authoritative = false
			m.Ns = matched
			for _, ns := range matched {
				nsRR, ok := ns.(*mdns.NS)
				if !ok {
					continue
				}
				for _, rr := range srv.records {
					switch g := rr.(type) {
					case *mdns.A:
						if g.Header().Name == nsRR.Ns {
							m.Extra = append(m.Extra, g)
						}
					case *mdns.AAAA:
						if g.Header().Name == nsRR.Ns {
							m.Extra = append(m.Extra, g)
						}
					}
				}
			}
		} else if srv.referral && len(matched) == 0 {
			// No exact match — look for a parent zone to refer to.
			// Walk up the qname labels to find a zone we can delegate.
			// e.g., query for "ns1.other.test." → find NS for "other.test."
			referralNS, referralExtra := findReferral(srv.records, qname)
			if len(referralNS) > 0 {
				m.Authoritative = false
				m.Ns = referralNS
				m.Extra = referralExtra
			}
		} else {
			// Authoritative mode: records in Answer section.
			m.Authoritative = true
			m.RecursionAvailable = srv.options.RecursionAvailable
			m.Answer = matched

			// Add glue in Additional for NS queries — both A and AAAA
			// matching an NS hostname.
			if qtype == mdns.TypeNS {
				for _, ans := range m.Answer {
					nsRR, ok := ans.(*mdns.NS)
					if !ok {
						continue
					}
					for _, rr := range srv.records {
						switch g := rr.(type) {
						case *mdns.A:
							if g.Header().Name == nsRR.Ns {
								m.Extra = append(m.Extra, g)
							}
						case *mdns.AAAA:
							if g.Header().Name == nsRR.Ns {
								m.Extra = append(m.Extra, g)
							}
						}
					}
				}
			}
		}

		_ = w.WriteMsg(m)
	})

	server := &mdns.Server{
		Addr:      srv.addr,
		Net:       "udp",
		Handler:   mux,
		ReuseAddr: true,
	}
	srv.server = server

	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		// ListenAndServe returns a non-nil error on normal shutdown
		// (Stop closes the socket out from under it); there is nothing
		// useful to do with it in a test server, so discard it.
		_ = server.ListenAndServe()
	}()

	// Wait for server to be ready (skip for Drop/Garbage servers —
	// they won't respond to readiness checks)
	if srv.options.Drop || srv.options.Garbage || srv.options.DropFirstN > 0 {
		time.Sleep(50 * time.Millisecond)
		return
	}
	client := &mdns.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		msg := new(mdns.Msg)
		msg.SetQuestion(".", mdns.TypeNS)
		_, _, err := client.Exchange(msg, srv.addr)
		if err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("DNS server at %s did not become ready", srv.addr)
}

// findReferral walks up the labels of qname to find NS records
// for a parent zone in the server's record set. Returns the NS
// records and any matching glue A records.
func findReferral(records []mdns.RR, qname string) (ns []mdns.RR, extra []mdns.RR) {
	// Walk up: "ns1.other.test." → "other.test." → "test." → "."
	name := qname
	for {
		idx := 0
		for idx < len(name) && name[idx] != '.' {
			idx++
		}
		if idx >= len(name) {
			break
		}
		name = name[idx+1:] // strip first label
		if name == "" {
			break
		}

		// Look for NS records matching this parent zone
		for _, rr := range records {
			if nsRR, ok := rr.(*mdns.NS); ok && nsRR.Header().Name == name {
				ns = append(ns, rr)
			}
		}
		if len(ns) > 0 {
			// Found a referral — collect glue (both A and AAAA matching
			// the NS hostname, mirroring real-world Additional sections).
			for _, nsRR := range ns {
				nsName := nsRR.(*mdns.NS).Ns
				for _, rr := range records {
					switch g := rr.(type) {
					case *mdns.A:
						if g.Header().Name == nsName {
							extra = append(extra, g)
						}
					case *mdns.AAAA:
						if g.Header().Name == nsName {
							extra = append(extra, g)
						}
					}
				}
			}
			return ns, extra
		}
	}
	return nil, nil
}
