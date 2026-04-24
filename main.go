package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promslog"
	promslogflag "github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"

	"github.com/sjr/dnshealth_exporter/config"
	"github.com/sjr/dnshealth_exporter/prober"
)

var (
	configFile = kingpin.Flag("config.file", "Path to configuration file.").
			Default("dnshealth.yml").String()
	webConfig = webflag.AddFlags(kingpin.CommandLine, ":9199")
)

func main() {
	promslogConfig := &promslog.Config{}
	promslogflag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.Version(version.Print("dnshealth_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := promslog.New(promslogConfig)

	logger.Info("Starting dnshealth_exporter", "version", version.Info())

	cfg, err := config.Load(*configFile)
	if err != nil {
		logger.Error("Failed to load config", "err", err)
		os.Exit(1)
	}

	logger.Info("Loaded configuration", "zones", len(cfg.Zones))

	// Wire up address resolution from config
	if len(cfg.AddressOverrides) > 0 {
		prober.ResolveAddress = cfg.ResolveAddress
		logger.Info("Address overrides configured", "count", len(cfg.AddressOverrides))
	}

	// Permanent registry for build info and operational counters
	permanentRegistry := prometheus.NewRegistry()
	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dnshealth_build_info",
		Help: "Build information for the exporter.",
		ConstLabels: prometheus.Labels{
			"version":   version.Version,
			"revision":  version.Revision,
			"goversion": version.GoVersion,
		},
	})
	buildInfo.Set(1)
	permanentRegistry.MustRegister(buildInfo)

	// Run initial probe (temporary — will be replaced by cycle runner)
	cycleRegistry := runAllProbes(context.Background(), cfg, logger)

	// Set up HTTP server
	mux := http.NewServeMux()
	// Gather from both permanent and cycle registries
	gatherers := prometheus.Gatherers{permanentRegistry, cycleRegistry}
	mux.Handle("/metrics", promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{}))
	mux.HandleFunc("/-/healthy", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html>
<head><title>DNS Health Exporter</title></head>
<body>
<h1>DNS Health Exporter</h1>
<p><a href="/metrics">Metrics</a></p>
</body>
</html>`)
	})

	server := &http.Server{Handler: mux}

	// Signal handling
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		logger.Info("Listening", "address", (*webConfig).WebListenAddresses)
		if err := web.ListenAndServe(server, webConfig, logger); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("Shutdown error", "err", err)
	}

	logger.Info("Shutdown complete")
}

func runAllProbes(ctx context.Context, cfg *config.Config, logger *slog.Logger) *prometheus.Registry {
	client := &dns.Client{Timeout: 5 * time.Second}
	var allResults []prober.ProbeResult
	for _, zone := range cfg.Zones {
		for name, fn := range prober.Probers {
			logger.Debug("Running probe", "check", name, "zone", zone)
			results, err := fn(ctx, zone, client, logger)
			if err != nil {
				logger.Warn("probe failed", "check", name, "zone", zone, "err", err)
				continue
			}
			allResults = append(allResults, results...)
		}
	}
	return prober.BuildRegistry(allResults)
}
