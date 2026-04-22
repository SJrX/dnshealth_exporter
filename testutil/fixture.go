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

type testServer struct {
	addr     string
	records  []mdns.RR
	referral bool // if true, return NS records as referrals (Authority section)
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

// Probe calls a prober function against a zone and returns the
// registry containing the registered metrics.
func (f *DNSFixture) Probe(fn prober.ProbeFn, zone string) *prometheus.Registry {
	f.t.Helper()
	registry := prometheus.NewRegistry()
	client := &mdns.Client{Timeout: 5 * time.Second}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := context.Background()

	if err := fn(ctx, zone, client, registry, logger); err != nil {
		f.t.Logf("Probe returned error: %v", err)
	}
	return registry
}

func startTestServer(t testing.TB, srv *testServer) {
	t.Helper()

	mux := mdns.NewServeMux()
	mux.HandleFunc(".", func(w mdns.ResponseWriter, r *mdns.Msg) {
		m := new(mdns.Msg)
		m.SetReply(r)

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

		if srv.referral && qtype == mdns.TypeNS {
			// Referral mode: NS records go in Authority, glue in Additional.
			// Response is NOT authoritative (simulates parent delegation).
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
		} else {
			// Authoritative mode: records in Answer section.
			m.Authoritative = true
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

	// Wait for server to be ready
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
