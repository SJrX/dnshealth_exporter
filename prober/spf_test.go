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

// TestParseSPFMechanisms covers classification of every lookup kind,
// target extraction, hasAll detection, qualifier stripping, and macro
// flagging — the inputs the lookup counter consumes.
func TestParseSPFMechanisms(t *testing.T) {
	mechs, hasAll := parseSPFMechanisms("v=spf1 ip4:192.0.2.0/24 a mx ptr exists:%{i}.e.net include:_spf.example.net redirect=_r.example.net ?all")

	if !hasAll {
		t.Errorf("hasAll = false, want true")
	}

	// Count kinds.
	kinds := map[spfMechKind]int{}
	var includeTarget, redirectTarget string
	for _, m := range mechs {
		kinds[m.kind]++
		if m.kind == mechInclude {
			includeTarget = m.target
		}
		if m.kind == mechRedirect {
			redirectTarget = m.target
		}
	}

	for kind, want := range map[spfMechKind]int{
		mechA: 1, mechMX: 1, mechPTR: 1, mechExists: 1,
		mechInclude: 1, mechRedirect: 1, mechAll: 1, mechOther: 1, // ip4 → other
	} {
		if kinds[kind] != want {
			t.Errorf("kind %d count = %d, want %d", kind, kinds[kind], want)
		}
	}
	if includeTarget != "_spf.example.net" {
		t.Errorf("include target = %q, want _spf.example.net", includeTarget)
	}
	if redirectTarget != "_r.example.net" {
		t.Errorf("redirect target = %q, want _r.example.net", redirectTarget)
	}
}

func TestParseSPFMechanisms_QualifiersAndMacros(t *testing.T) {
	// Qualifiers are stripped; a macro target is flagged.
	mechs, _ := parseSPFMechanisms("v=spf1 -include:%{ir}._spf.example.net ~mx +a")

	if len(mechs) != 3 {
		t.Fatalf("got %d mechanisms, want 3", len(mechs))
	}
	if mechs[0].kind != mechInclude || !mechs[0].hasMacro {
		t.Errorf("mech[0] = %+v, want include with hasMacro=true", mechs[0])
	}
	if mechs[1].kind != mechMX {
		t.Errorf("mech[1] kind = %d, want mechMX (qualifier ~ stripped)", mechs[1].kind)
	}
	if mechs[2].kind != mechA {
		t.Errorf("mech[2] kind = %d, want mechA (qualifier + stripped)", mechs[2].kind)
	}
}
