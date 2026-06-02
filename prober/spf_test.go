package prober

import "testing"

// TestAnalyzeSPF exercises the pure SPF parser across presence, the
// multiple-record PermError, every terminal qualifier, multi-string
// concatenation (the caller concatenates per-RR; here each slice element
// is one already-concatenated record), unrelated-TXT rejection, and the
// narrow R-9 malformed boundary — including the positive case that an
// unknown-but-harmless mechanism is TOLERATED, not false-FAILed.
func TestAnalyzeSPF(t *testing.T) {
	tests := []struct {
		name      string
		records   []string
		present   bool
		count     int
		valid     bool
		qualifier string
	}{
		{
			name:      "healthy hard-fail",
			records:   []string{"v=spf1 include:_spf.example.net -all"},
			present:   true,
			count:     1,
			valid:     true,
			qualifier: "fail",
		},
		{
			name:      "softfail",
			records:   []string{"v=spf1 ~all"},
			present:   true,
			count:     1,
			valid:     true,
			qualifier: "softfail",
		},
		{
			name:      "neutral",
			records:   []string{"v=spf1 ?all"},
			present:   true,
			count:     1,
			valid:     true,
			qualifier: "neutral",
		},
		{
			name:      "permissive plus-all",
			records:   []string{"v=spf1 +all"},
			present:   true,
			count:     1,
			valid:     true,
			qualifier: "pass",
		},
		{
			name:      "bare all is pass",
			records:   []string{"v=spf1 mx all"},
			present:   true,
			count:     1,
			valid:     true,
			qualifier: "pass",
		},
		{
			name:      "no terminal all",
			records:   []string{"v=spf1 include:_spf.example.net"},
			present:   true,
			count:     1,
			valid:     true,
			qualifier: "none",
		},
		{
			name:    "absent",
			records: []string{"some-verification-token=abc123"},
			present: false,
			count:   0,
			valid:   false,
		},
		{
			name:    "empty",
			records: nil,
			present: false,
			count:   0,
			valid:   false,
		},
		{
			name:      "multiple records is invalid",
			records:   []string{"v=spf1 -all", "v=spf1 include:_spf.example.net ~all"},
			present:   true,
			count:     2,
			valid:     false,
			qualifier: "", // undefined when multiple
		},
		{
			name:      "case-insensitive version token",
			records:   []string{"V=SPF1 -all"},
			present:   true,
			count:     1,
			valid:     true,
			qualifier: "fail",
		},
		{
			name:      "unrelated TXT ignored, SPF selected",
			records:   []string{"google-site-verification=xyz", "v=spf1 -all"},
			present:   true,
			count:     1,
			valid:     true,
			qualifier: "fail",
		},
		{
			name:      "unknown mechanism is tolerated (not false-FAIL)",
			records:   []string{"v=spf1 ip4:192.0.2.0/24 unknownmech -all"},
			present:   true,
			count:     1,
			valid:     true,
			qualifier: "fail",
		},
		{
			name:      "malformed: bare qualifier",
			records:   []string{"v=spf1 - -all"},
			present:   true,
			count:     1,
			valid:     false,
			qualifier: "fail",
		},
		{
			name:      "malformed: empty include target",
			records:   []string{"v=spf1 include: -all"},
			present:   true,
			count:     1,
			valid:     false,
			qualifier: "fail",
		},
		{
			name:    "v=spf10 is not an SPF record",
			records: []string{"v=spf10 something"},
			present: false,
			count:   0,
			valid:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := analyzeSPF(tc.records)

			if got.present != tc.present {
				t.Errorf("present = %v, want %v", got.present, tc.present)
			}
			if got.recordCount != tc.count {
				t.Errorf("recordCount = %d, want %d", got.recordCount, tc.count)
			}
			if got.valid != tc.valid {
				t.Errorf("valid = %v, want %v", got.valid, tc.valid)
			}
			if got.qualifier != tc.qualifier {
				t.Errorf("qualifier = %q, want %q", got.qualifier, tc.qualifier)
			}
		})
	}
}
