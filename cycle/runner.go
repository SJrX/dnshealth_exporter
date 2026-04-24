package cycle

import (
	"context"
	"log/slog"

	"github.com/sjr/dnshealth_exporter/cache"
	"github.com/sjr/dnshealth_exporter/config"
	"github.com/sjr/dnshealth_exporter/prober"
)

// Runner executes probe cycles.
type Runner struct {
	Cache  *cache.DelegationCache
	Logger *slog.Logger
}

// Run executes a single probe cycle: fans out zone probes concurrently,
// collects all results, and returns them.
func (r *Runner) Run(ctx context.Context, cfg *config.Config) []prober.ProbeResult {
	// TODO: implement scatter-gather
	return nil
}
