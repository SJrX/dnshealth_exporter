package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promslog"
	promslogflag "github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"

	"github.com/sjr/dnshealth_exporter/cache"
	"github.com/sjr/dnshealth_exporter/config"
	"github.com/sjr/dnshealth_exporter/cycle"
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

	// Wire up root server override from config (used by the demo
	// deployment to walk delegation against an in-stack fake root).
	if len(cfg.RootServers) > 0 {
		prober.RootServers = cfg.RootServers
		logger.Info("Root server override configured", "count", len(cfg.RootServers))
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

	// Cycle state: atomic pointer to the current cycle's registry.
	// nil before first cycle completes → /metrics returns 503.
	var cycleRegistry atomic.Pointer[prometheus.Registry]

	// Delegation cache
	delegationCache := cache.NewDelegationCache(cfg.DelegationCacheTTL)

	// Cycle runner with operational metrics on permanent registry
	runner := cycle.NewRunner(delegationCache, logger, permanentRegistry)

	// Atomic config pointer for reload support
	var currentConfig atomic.Pointer[config.Config]
	currentConfig.Store(cfg)

	// Background probe loop
	cycleRunning := make(chan struct{}, 1)
	go func() {
		// Run first cycle immediately
		runCycle(runner, &currentConfig, &cycleRegistry, cycleRunning, logger)

		ticker := time.NewTicker(cfg.ProbeInterval)
		defer ticker.Stop()
		for range ticker.C {
			runCycle(runner, &currentConfig, &cycleRegistry, cycleRunning, logger)
		}
	}()

	// Set up HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		reg := cycleRegistry.Load()
		if reg == nil {
			http.Error(w, "Probe cycle not yet complete", http.StatusServiceUnavailable)
			return
		}
		gatherers := prometheus.Gatherers{permanentRegistry, reg}
		promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	})
	mux.HandleFunc("/-/healthy", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})
	mux.HandleFunc("/-/reload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		newCfg, err := config.Load(*configFile)
		if err != nil {
			logger.Error("Config reload failed", "err", err)
			http.Error(w, fmt.Sprintf("Reload failed: %v", err), http.StatusInternalServerError)
			return
		}
		applyReloadedConfig(newCfg, &currentConfig, delegationCache)
		logger.Info("Configuration reloaded", "zones", len(newCfg.Zones))
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

	// Signal handling: SIGTERM/SIGINT for shutdown, SIGHUP for reload
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go func() {
		for range sighup {
			newCfg, err := config.Load(*configFile)
			if err != nil {
				logger.Error("Config reload via SIGHUP failed", "err", err)
				continue
			}
			applyReloadedConfig(newCfg, &currentConfig, delegationCache)
			logger.Info("Configuration reloaded via SIGHUP", "zones", len(newCfg.Zones))
		}
	}()

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

// applyReloadedConfig swaps in a freshly-loaded config: stores the
// pointer, rebinds prober.ResolveAddress and prober.RootServers
// (unconditionally — removing overrides via reload must take effect,
// so when the new config has none we restore the prober defaults), and
// invalidates the delegation cache so the next cycle re-walks.
func applyReloadedConfig(newCfg *config.Config, current *atomic.Pointer[config.Config], delegationCache *cache.DelegationCache) {
	current.Store(newCfg)
	prober.ResolveAddress = newCfg.ResolveAddress
	if len(newCfg.RootServers) > 0 {
		prober.RootServers = newCfg.RootServers
	} else {
		// Copy so callers cannot inadvertently mutate the canonical
		// DefaultRootServers via the active RootServers slice.
		prober.RootServers = append([]string(nil), prober.DefaultRootServers...)
	}
	delegationCache.Invalidate()
}

func runCycle(runner *cycle.Runner, cfgPtr *atomic.Pointer[config.Config], registryPtr *atomic.Pointer[prometheus.Registry], running chan struct{}, logger *slog.Logger) {
	// Overlap prevention: skip if previous cycle still running
	select {
	case running <- struct{}{}:
		// acquired
	default:
		logger.Warn("Skipping probe cycle — previous cycle still running")
		return
	}
	defer func() { <-running }()

	cfg := cfgPtr.Load()
	logger.Info("Starting probe cycle", "zones", len(cfg.Zones))

	result := runner.Run(context.Background(), cfg)

	registry := prober.BuildRegistry(result.Results)
	registryPtr.Store(registry)

	logger.Info("Probe cycle complete",
		"duration", result.Duration,
		"zones", result.ZoneCount)
}
