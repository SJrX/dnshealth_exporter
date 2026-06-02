package prober

import "testing"

// TestAnalyzeDMARC exercises the pure DMARC parser: presence, every
// policy value, the malformed case (v=DMARC1 with no p=), optional
// sp=/rua=/ruf=, case-insensitive tags, and unrelated-TXT rejection.
func TestAnalyzeDMARC(t *testing.T) {
	tests := []struct {
		name    string
		records []string
		present bool
		valid   bool
		policy  string
		sp      string
		rua     bool
		ruf     bool
	}{
		{
			name:    "reject with reporting",
			records: []string{"v=DMARC1; p=reject; rua=mailto:dmarc@example.com"},
			present: true,
			valid:   true,
			policy:  "reject",
			rua:     true,
		},
		{
			name:    "quarantine",
			records: []string{"v=DMARC1; p=quarantine"},
			present: true,
			valid:   true,
			policy:  "quarantine",
		},
		{
			name:    "monitoring only p=none",
			records: []string{"v=DMARC1; p=none"},
			present: true,
			valid:   true,
			policy:  "none",
		},
		{
			name:    "absent",
			records: nil,
			present: false,
			valid:   false,
		},
		{
			name:    "unrelated TXT ignored",
			records: []string{"some-token=abc"},
			present: false,
			valid:   false,
		},
		{
			name:    "malformed: no p= tag",
			records: []string{"v=DMARC1; rua=mailto:dmarc@example.com"},
			present: true,
			valid:   false,
			policy:  "",
			rua:     true,
		},
		{
			name:    "malformed: invalid p= value",
			records: []string{"v=DMARC1; p=bogus"},
			present: true,
			valid:   false,
			policy:  "",
		},
		{
			name:    "subdomain policy + forensic reporting",
			records: []string{"v=DMARC1; p=reject; sp=quarantine; ruf=mailto:f@example.com"},
			present: true,
			valid:   true,
			policy:  "reject",
			sp:      "quarantine",
			ruf:     true,
		},
		{
			name:    "case-insensitive tags and version",
			records: []string{"v=dmarc1; P=Reject"},
			present: true,
			valid:   true,
			policy:  "reject",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := analyzeDMARC(tc.records)

			if got.present != tc.present {
				t.Errorf("present = %v, want %v", got.present, tc.present)
			}
			if got.valid != tc.valid {
				t.Errorf("valid = %v, want %v", got.valid, tc.valid)
			}
			if got.policy != tc.policy {
				t.Errorf("policy = %q, want %q", got.policy, tc.policy)
			}
			if got.subdomainPolicy != tc.sp {
				t.Errorf("subdomainPolicy = %q, want %q", got.subdomainPolicy, tc.sp)
			}
			if got.rua != tc.rua {
				t.Errorf("rua = %v, want %v", got.rua, tc.rua)
			}
			if got.ruf != tc.ruf {
				t.Errorf("ruf = %v, want %v", got.ruf, tc.ruf)
			}
		})
	}
}
