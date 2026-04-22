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
	// Using a high port avoids needing root privileges.
	TestPort = "10053"
)

// DNSFixture manages in-process DNS servers for integration tests.
type DNSFixture struct {
	t       testing.TB
	servers []*testServer
}

type testServer struct {
	addr    string
	records []mdns.RR
	server  *mdns.Server
	wg      sync.WaitGroup
}

// NewDNSFixture creates a new fixture manager.
func NewDNSFixture(t testing.TB) *DNSFixture {
	return &DNSFixture{t: t}
}

// Server adds an in-process DNS server at the given address
// (e.g., "127.240.0.2:10053") serving the provided records.
// Records are served for any query matching the zone.
func (f *DNSFixture) Server(addr string, records ...mdns.RR) *DNSFixture {
	f.t.Helper()
	f.servers = append(f.servers, &testServer{
		addr:    addr,
		records: records,
	})
	return f
}

// Start launches all configured DNS servers. Call this after all
// Server() calls. Returns the fixture for chaining.
func (f *DNSFixture) Start(t testing.TB) *DNSFixture {
	t.Helper()
	for _, srv := range f.servers {
		startTestServer(t, srv)
	}
	// Brief pause to let servers bind
	time.Sleep(50 * time.Millisecond)
	return f
}

// Stop shuts down all DNS servers. Typically called via defer.
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
		m.Authoritative = true

		if len(r.Question) == 0 {
			w.WriteMsg(m)
			return
		}

		qname := r.Question[0].Name
		qtype := r.Question[0].Qtype

		for _, rr := range srv.records {
			if rr.Header().Name != qname {
				continue
			}
			if qtype == mdns.TypeANY || rr.Header().Rrtype == qtype {
				m.Answer = append(m.Answer, rr)
			}
		}

		// Add glue records in the Additional section for NS queries
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

	// Wait for the server to be ready by doing a test query
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
