package prober

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/miekg/dns"
)

// IsTimeout returns true if err (or any error it wraps) is a
// network timeout. Uses errors.As so wrapped errors from
// fmt.Errorf("...%w", err) are detected.
func IsTimeout(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// ExchangeWithRetry performs a DNS exchange with one retry on
// transient failure (timeout, network error). Non-transient
// failures (NXDOMAIN, REFUSED) are not retried.
// The retry uses half the original timeout.
func ExchangeWithRetry(ctx context.Context, client *dns.Client, msg *dns.Msg, addr string) (*dns.Msg, time.Duration, error) {
	resp, rtt, err := client.ExchangeContext(ctx, msg, addr)
	if err == nil {
		return resp, rtt, nil
	}

	// Don't retry if context is cancelled
	if ctx.Err() != nil {
		return nil, 0, err
	}

	// Retry once with half timeout
	retryClient := &dns.Client{
		Timeout: client.Timeout / 2,
	}
	resp, rtt, retryErr := retryClient.ExchangeContext(ctx, msg, addr)
	if retryErr != nil {
		// Return original error (more informative)
		return nil, 0, err
	}
	return resp, rtt, nil
}
