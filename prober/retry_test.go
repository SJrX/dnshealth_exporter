package prober_test

import (
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
)

// fakeNetErr is a net.Error with a configurable Timeout() value, used
// to keep the test independent of OS-level networking.
type fakeNetErr struct {
	msg     string
	timeout bool
}

func (e *fakeNetErr) Error() string   { return e.msg }
func (e *fakeNetErr) Timeout() bool   { return e.timeout }
func (e *fakeNetErr) Temporary() bool { return false }

func TestIsTimeout(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "plain timeout net.Error",
			err:  &fakeNetErr{msg: "i/o timeout", timeout: true},
			want: true,
		},
		{
			name: "wrapped timeout via fmt.Errorf %w",
			err:  fmt.Errorf("querying NS at 1.2.3.4: %w", &fakeNetErr{msg: "i/o timeout", timeout: true}),
			want: true,
		},
		{
			name: "doubly wrapped timeout",
			err:  fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", &fakeNetErr{msg: "i/o timeout", timeout: true})),
			want: true,
		},
		{
			name: "non-timeout net.Error",
			err:  &fakeNetErr{msg: "connection refused", timeout: false},
			want: false,
		},
		{
			name: "wrapped non-timeout net.Error",
			err:  fmt.Errorf("wrapper: %w", &fakeNetErr{msg: "connection refused", timeout: false}),
			want: false,
		},
		{
			name: "non-net error",
			err:  errors.New("something else"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prober.IsTimeout(tt.err); got != tt.want {
				t.Errorf("prober.IsTimeout(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestIsTimeout_RealNetTimeout asserts behavior against a real
// net.OpError carrying a timeout, just to confirm our errors.As path
// matches what the stdlib produces — fakeNetErr is convenient but the
// "real" path is the one we care about.
func TestIsTimeout_RealNetTimeout(t *testing.T) {
	realErr := &net.OpError{
		Op:  "read",
		Net: "udp",
		Err: &timeoutError{},
	}
	if !prober.IsTimeout(realErr) {
		t.Errorf("IsTimeout on real net.OpError with timeout: got false, want true")
	}
	wrapped := fmt.Errorf("dns exchange failed: %w", realErr)
	if !prober.IsTimeout(wrapped) {
		t.Errorf("IsTimeout on wrapped real net.OpError: got false, want true")
	}
}

// timeoutError mimics the stdlib internal/poll.TimeoutError shape.
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }
