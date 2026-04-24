package testutil

import (
	"context"
	"log/slog"
	"os"
	"sync"
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
}

type testServer struct {
	addr     string
	records  []mdns.RR
	referral bool // if true, return NS records as referrals (Authority section)
	options  ServerOptions
	server   *mdns.Server
	wg       sync.WaitGroup
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
			srv.server.ShutdownContext(context.Background())
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

	var nameservers []prober.Nameserver
	for _, ns := range delegation.NSRecords {
		if ns.IP != "" {
			nameservers = append(nameservers, ns)
			continue
		}
		ip, err := prober.ResolveHostname(ctx, ns.Hostname, client, logger)
		if err != nil {
			f.t.Logf("Resolve hostname error: %v", err)
			continue
		}
		nameservers = append(nameservers, prober.Nameserver{Hostname: ns.Hostname, IP: ip})
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
		// Drop: silently ignore the query (client will timeout)
		if srv.options.Drop {
			return
		}

		// Garbage: send random bytes
		if srv.options.Garbage {
			w.Write([]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01, 0x02, 0x03})
			return
		}

		m := new(mdns.Msg)
		m.SetReply(r)

		// Override rcode if set
		if srv.options.Rcode != 0 {
			m.Rcode = srv.options.Rcode
			w.WriteMsg(m)
			return
		}

		if len(r.Question) == 0 {
			w.WriteMsg(m)
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
			m.Authoritative = false
			m.Ns = matched
			for _, ns := range matched {
				nsRR, ok := ns.(*mdns.NS)
				if !ok {
					continue
				}
				for _, rr := range srv.records {
					if a, ok := rr.(*mdns.A); ok && a.Header().Name == nsRR.Ns {
						m.Extra = append(m.Extra, a)
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

			// Add glue in Additional for NS queries
			if qtype == mdns.TypeNS {
				for _, ans := range m.Answer {
					nsRR, ok := ans.(*mdns.NS)
					if !ok {
						continue
					}
					for _, rr := range srv.records {
						if a, ok := rr.(*mdns.A); ok && a.Header().Name == nsRR.Ns {
							m.Extra = append(m.Extra, a)
						}
					}
				}
			}
		}

		w.WriteMsg(m)
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
		if err := server.ListenAndServe(); err != nil {
			// Ignore errors from shutdown
		}
	}()

	// Wait for server to be ready (skip for Drop/Garbage servers —
	// they won't respond to readiness checks)
	if srv.options.Drop || srv.options.Garbage {
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
			// Found a referral — collect glue
			for _, nsRR := range ns {
				nsName := nsRR.(*mdns.NS).Ns
				for _, rr := range records {
					if a, ok := rr.(*mdns.A); ok && a.Header().Name == nsName {
						extra = append(extra, a)
					}
				}
			}
			return ns, extra
		}
	}
	return nil, nil
}
