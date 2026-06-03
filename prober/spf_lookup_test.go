package prober

import "testing"

// fakeSPF builds a fetchSPFFunc backed by a static map (no DNS). A name
// absent from the map resolves as "no SPF record" (ok=false) — the
// unreachable-include case.
func fakeSPF(records map[string]string) fetchSPFFunc {
	return func(name string) (string, bool) {
		r, ok := records[name]
		return r, ok
	}
}

// TestCountSPFLookups exercises the bounded recursive counter via an
// injected fake fetch — no DNS. Covers in-budget exact counts, the
// recurse-only-include/redirect rule, redirect precedence, stop-at-11,
// cycles, depth, and the graceful-degradation (eval-incomplete) paths.
func TestCountSPFLookups(t *testing.T) {
	tests := []struct {
		name     string
		record   string
		records  map[string]string // include/redirect targets
		count    int
		complete bool
	}{
		{
			name:     "no lookups",
			record:   "v=spf1 ip4:192.0.2.0/24 -all",
			count:    0,
			complete: true,
		},
		{
			name:     "a/mx/ptr/exists counted without fetch",
			record:   "v=spf1 a mx ptr exists:%{ir}.example.net -all",
			count:    4,
			complete: true,
		},
		{
			name:     "single include recurses",
			record:   "v=spf1 include:_spf.example.net -all",
			records:  map[string]string{"_spf.example.net": "v=spf1 a mx -all"},
			count:    3, // include(1) + a(1) + mx(1)
			complete: true,
		},
		{
			name:   "exactly 11 → over budget, reported as 11",
			record: "v=spf1 include:_a include:_b include:_c -all",
			records: map[string]string{
				"_a": "v=spf1 a mx a mx -all", // 4
				"_b": "v=spf1 a mx a mx -all", // 4
				"_c": "v=spf1 a mx -all",      // 2
			},
			// 3 includes + 4 + 4 = 11 reached partway through _c → stop
			count:    11,
			complete: true,
		},
		{
			name:   "deep over-budget stops at 11",
			record: "v=spf1 include:_a -all",
			records: map[string]string{
				"_a": "v=spf1 a a a a a a a a a a a a a a a a -all", // 16 a's
			},
			count:    11,
			complete: true,
		},
		{
			name:     "redirect followed when no all",
			record:   "v=spf1 redirect=_r.example.net",
			records:  map[string]string{"_r.example.net": "v=spf1 a mx -all"},
			count:    3, // redirect(1) + a(1) + mx(1)
			complete: true,
		},
		{
			name:     "redirect ignored when all present",
			record:   "v=spf1 a redirect=_r.example.net -all",
			records:  map[string]string{"_r.example.net": "v=spf1 a a a -all"},
			count:    1, // just the a; redirect not followed (all present)
			complete: true,
		},
		{
			name:   "cyclic include terminates, reported over budget + incomplete",
			record: "v=spf1 include:_a -all",
			records: map[string]string{
				"_a": "v=spf1 include:_b -all",
				"_b": "v=spf1 include:_a -all",
			},
			count:    11,
			complete: false,
		},
		{
			name:     "unreachable include ⇒ incomplete, not over budget (US2)",
			record:   "v=spf1 include:_missing a -all",
			records:  map[string]string{}, // _missing absent
			count:    2,                   // include(1) + a(1); no sub-count
			complete: false,
		},
		{
			name:   "unreachable include on an already-over-budget record still FAILs",
			record: "v=spf1 a a a a a a a a a a a include:_missing -all", // 11 a's already
			count:  11,
			// _missing never reached (stopped at 11); but the point is
			// exceeded stands. complete stays true because we stopped
			// before touching the missing include.
			complete: true,
		},
		{
			name:     "macro include target ⇒ incomplete",
			record:   "v=spf1 include:%{ir}._spf.example.net a -all",
			count:    2, // include(1) + a(1)
			complete: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count, complete := countSPFLookups(tc.record, fakeSPF(tc.records))
			if count != tc.count {
				t.Errorf("count = %d, want %d", count, tc.count)
			}
			if complete != tc.complete {
				t.Errorf("complete = %v, want %v", complete, tc.complete)
			}
		})
	}
}

// TestCountSPFLookups_BoundedFetches asserts the walk stops fetching once
// it knows the record is over budget (stop-at-11) rather than walking a
// pathological fan-out to completion.
func TestCountSPFLookups_BoundedFetches(t *testing.T) {
	// 20 includes, each cheap; the walk should stop after ~11 and not
	// fetch all 20.
	record := "v=spf1"
	records := map[string]string{}
	for _, n := range []string{"_i1", "_i2", "_i3", "_i4", "_i5", "_i6", "_i7", "_i8", "_i9", "_i10", "_i11", "_i12", "_i13", "_i14", "_i15", "_i16", "_i17", "_i18", "_i19", "_i20"} {
		record += " include:" + n
		records[n] = "v=spf1 -all" // each include = 1 lookup, no sub-lookups
	}
	record += " -all"

	fetches := 0
	fetch := func(name string) (string, bool) {
		fetches++
		r, ok := records[name]
		return r, ok
	}

	count, complete := countSPFLookups(record, fetch)
	if count != 11 {
		t.Errorf("count = %d, want 11", count)
	}
	if !complete {
		t.Errorf("complete = %v, want true", complete)
	}
	// 11 includes push the count over budget; the walk must not fetch all 20.
	if fetches > 11 {
		t.Errorf("fetched %d include targets, want <= 11 (stop-at-11 not bounding the walk)", fetches)
	}
}
