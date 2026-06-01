# Phase 0 Research: Email-Authentication DNS Records (Tier 1: SPF + DMARC)

All decisions below resolve the unknowns implied by the spec's functional requirements. No `NEEDS CLARIFICATION` markers remain after `/speckit.clarify` (severity model and MX-independence are settled in the spec's Clarifications section).

---

## R-1 — How to query SPF and DMARC records

**Decision**: Issue a single `TypeTXT` query per record, against the zone's authoritative nameservers (the `nameservers []Nameserver` slice the runner already resolves and passes to every prober), exactly as `soa`/`mx` do.
- SPF: TypeTXT at the zone apex (`<zone>`).
- DMARC: TypeTXT at `_dmarc.<zone>`.

Use the first authoritative server that answers (matching the existing per-prober "ask the auth" pattern); a per-server divergence check is **out of scope** for Tier 1 (SPF/DMARC are zone data, expected identical across the zone's servers — unlike SOA serials, divergence here is rare and not a Tier-1 concern).

**Rationale**: SPF/DMARC are published *in the zone*, so the authoritative servers are the source of truth. Reuses `ExchangeWithRetry` and the existing timeout/retry plumbing. No recursive resolver needed for the records themselves.

**Alternatives considered**: Query via the configured recursive path / delegation walk — unnecessary for records that live in the zone we already have authoritative addresses for; adds latency and failure surface.

## R-2 — Multi-string TXT concatenation and record selection

**Decision**: A single DNS TXT record can carry multiple character-strings (each ≤255 bytes); `miekg/dns` exposes them as `[]string` on `*dns.TXT.Txt`. Concatenate the strings of **one** RR with no separator before parsing (RFC 7208 §3.3, RFC 7489 §A.5). Across the multiple TXT *RRs* at a name, select email-auth records by case-insensitive prefix: `v=spf1` (followed by space or end) for SPF, `v=DMARC1` for DMARC. Ignore all other TXT RRs (verification tokens, etc.).

**Rationale**: Directly required by FR-004 and the edge cases. The "concatenate strings within an RR, but treat separate RRs as separate records" distinction is the subtle correctness point — getting it backwards either splits a long SPF record into garbage or merges two SPF records into one (hiding the multiple-record error).

**Alternatives considered**: Treating each character-string as a record (wrong — breaks long records); naive whole-response concatenation (wrong — merges distinct RRs).

## R-3 — SPF parsing: pure hand-rolled parser, no dependency (decided)

**Decision**: Hand-roll a small **pure** SPF parser (`spf.go`) — no DNS, no recursion, no dependency. v1 extracts exactly the three things the dashboard rows need:
- **Presence + record count**: select TXT RRs whose concatenated string begins (case-insensitively) with `v=spf1` (R-2). Zero = absent; one = the record; more than one = the RFC 7208 §3.2 multiple-record PermError. This is the whole of row A and is trivial string work.
- **Terminal `all` qualifier** (row B): split the single record on whitespace, find the last term matching `[-~?+]?all`, map its qualifier (`-`→fail, `~`→softfail, `?`→neutral, `+`/none→pass). No `all` term ⇒ "none". This is a **syntactic** read of the apex record only; we do **not** follow a `redirect=` to compute an effective qualifier (that needs resolution → deferred with the budget check, #58). When both `all` and `redirect` appear, `all` wins (RFC 7208) and we read the `all`.
- **Malformed** (row A FAIL): the narrow, well-tested cases the issue's failure modes actually hit — a record that begins `v=spf1` but is empty after the version, or whose terms are unsplittable. We deliberately keep "malformed" narrow and tolerate unknown-but-harmless terms to avoid false FAILs on valid-but-exotic records.

**Rationale**: With the lookup-budget check deferred (R-4), nothing left in SPF v1 needs the DNS recursion or macro/CIDR handling that motivated a library. Presence + multiple-record + terminal qualifier is ~40 lines of pure string parsing, table-driven unit-testable with zero DNS — strictly simpler than pulling and adapting an evaluator library. Matches how the project already hand-parses DNS data (e.g. `ns_hostname.go`).

**Alternatives considered**: `github.com/wttw/spf` — a full RFC 7208 evaluator that exposes `Result.DNSQueries`; genuinely the best fit *if* we needed the lookup budget, but it is a per-query stub consumer (we'd still build a recursive resolver to feed it) and it's a 2022 pre-1.0 dependency. Once the budget check is deferred, the library buys nothing v1 needs. Re-evaluate it for #58.

## R-4 — SPF DNS-lookup budget: DEFERRED to #58

**Decision**: The RFC 7208 §4.6.4 ten-lookup budget check is **out of scope for v1** (spec Clarifications §2026-05-31, tracked in [#58](https://github.com/SJrX/dnshealth_exporter/issues/58)). It is the only SPF check that requires recursively fetching and parsing `include`/`redirect` targets — and therefore a recursive resolver — which is the bulk of its cost. It is User Story 3 / Priority P3 (optional refinement). Deferring it keeps v1 to pure-string parsing with no new dependency and no recursive resolver.

**Rationale**: Concentrates 100% of SPF's complexity in one deferrable, lowest-priority story. The severity model already designates an over-budget record FAIL for when #58 lands; the metrics and dashboard row it adds are scoped in #58.

**Alternatives considered**: Shipping it in v1 via either a hand-rolled counter or `wttw/spf` — both require building the recursive resolver, which is the very complexity the user chose to defer.

## R-5 — DMARC parsing scope

**Decision**: Parse the `v=DMARC1` record into `tag=value` pairs split on `;`. Extract:
- `p=` → policy ∈ {none, quarantine, reject}. **Required**; a record present without a valid `p=` is **malformed** (RFC 7489 §6.3 requires `p` immediately after `v`).
- `sp=` → subdomain policy (optional, SHOULD-surface per FR-010).
- `rua=` / `ruf=` presence → aggregate/forensic reporting configured (optional).
NXDOMAIN and NODATA at `_dmarc.<zone>` are both "not present" (FR-007). Matching is case-insensitive on tag names and the version token; policy values are lowercased.

**Rationale**: Matches FR-007–FR-010. Keeps DMARC parsing to the enforcement-relevant tags; alignment modes (`adkim`/`aspf`), percentage (`pct`), and report-interval are not Tier-1 dashboard signals and are skipped.

**Alternatives considered**: Full RFC 7489 tag validation incl. external-domain `rua` authorization (`_report._dmarc` verification) — deferred per spec Assumptions.

## R-6 — Metric design

**Decision**: All gauges, `dnshealth_spf_*` / `dnshealth_dmarc_*`, per-zone. Value-bearing enums use the **info-gauge pattern** already used by `ns_classification` (value 1, meaning in a label):

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `dnshealth_spf_present` | gauge 0/1 | zone | a `v=spf1` record exists at apex |
| `dnshealth_spf_record_count` | gauge | zone | number of `v=spf1` RRs (>1 ⇒ PermError) |
| `dnshealth_spf_valid` | gauge 0/1 | zone | the single SPF record parsed without malformation |
| `dnshealth_spf_terminal_all` | gauge 1 (info) | zone, qualifier∈{fail,softfail,neutral,pass,none} | the (syntactic) terminal `all` qualifier |
| `dnshealth_dmarc_present` | gauge 0/1 | zone | a `v=DMARC1` record exists at `_dmarc` |
| `dnshealth_dmarc_valid` | gauge 0/1 | zone | present and has a valid `p=` tag |
| `dnshealth_dmarc_policy` | gauge 1 (info) | zone, policy∈{none,quarantine,reject} | the enforcement policy |
| `dnshealth_dmarc_sp_policy` | gauge 1 (info) | zone, policy | subdomain policy if `sp=` present (optional) |
| `dnshealth_dmarc_rua_present` / `_ruf_present` | gauge 0/1 | zone | reporting addresses configured (optional) |

Zero-emission (Reset+Set(0)) applies to the boolean per-zone gauges so a zone with no SPF reads `dnshealth_spf_present 0` rather than the series vanishing — matching the spec 007/008 pattern, so the dashboard predicates have a present series to test. Info gauges only emit for the state that applies (no `qualifier=none` AND `qualifier=fail` for the same zone).

**Rationale**: Every signal the four dashboard rows and the quickstart PromQL recipes need, with thresholds left to PromQL (Principle V/IX). Info-gauge enums keep cardinality bounded and queries label-selectable. (The deferred budget check, #58, adds its own `dnshealth_spf_lookup_*` gauges and a fifth row later.)

## R-7 — Dashboard: "Email auth — status" panel

**Decision**: A new status panel built with the existing `statusTable` builder, **four** four-state rows using `composeStatusExpr` (expr/naExpr/warnExpr), encoding the clarified severity model:

| Row | PASS | WARN | FAIL | N/A |
|-----|------|------|------|-----|
| A. SPF record present & well-formed | exactly 1 valid SPF | 0 SPF records (absent) | >1 record, or present-but-malformed | — |
| B. SPF ends in restrictive `all` | qualifier fail/softfail | neutral / none / **pass (`+all`)** | — | no SPF record |
| C. DMARC record present & valid | present + valid `p=` | absent | present but malformed (no `p=`) | — |
| D. DMARC enforces a policy | p=quarantine/reject | p=none | — | no DMARC record |

Each row carries detail text (guard-test enforced) that — per FR-017 — explains the anti-spoofing rationale, so a no-mail/Null-MX zone's WARN reads as a sane nudge, not a contradiction. Threshold PromQL lives in the dashboard; the predicates select on the info-gauge labels (e.g. row B WARN = `dnshealth_spf_terminal_all{qualifier=~"neutral|none|pass"}`). The deferred budget check (#58) will insert an "SPF within 10-lookup budget" row.

**Rationale**: Reuses the entire four-state machinery (Principle IX) validated by the drift + detail-guard + `promql_live` tests. The four-row split maps cleanly onto the two v1 user stories (A/B = US1, C/D = US2) and onto the severity clarification.

**Alternatives considered**: Folding SPF into one mega-row — loses the ability to distinguish "no SPF" (WARN) from "SPF broken" (FAIL) from "SPF too permissive" (WARN) at a glance, which is the whole operator value.

## R-8 — Demo zone construction

**Decision**: Six demo zones (per plan structure), all trivial static TXT records — no include chains in v1:
- `email-healthy` — `v=spf1 -all` at apex + `v=DMARC1; p=reject; rua=…` at `_dmarc`.
- `email-spf-only` — SPF `-all`, no `_dmarc` record.
- `email-none` — neither record.
- `email-permissive` — `v=spf1 +all` and/or `v=DMARC1; p=none`.
- `email-broken` — two `v=spf1` RRs at apex (multiple-record PermError) and/or a `_dmarc` record missing `p=` (malformed).
- `email-nomail` — Null MX (`0 .`) + `v=spf1 -all` + `v=DMARC1; p=reject` (proves FR-017: PASS independent of MX).

All are plain TXT records in each zone's CoreDNS file; no extra containers, no resolution chains.

**Rationale**: With the budget check deferred, every demo case is a static record the parser reads directly — keeps the demo simple and fully offline. (The over-budget include chain belongs to #58.)

**Alternatives considered**: Using real provider SPF records — violates the offline-demo invariant and the no-real-domains rule (FR-016).

## R-9 — Malformed-SPF definition boundary (scope guard)

**Decision**: v1 flags `dnshealth_spf_valid 0` (row A FAIL) for the narrow, well-tested cases the dashboard needs: more than one `v=spf1` RR (the §3.2 PermError, counted directly), or a single record that is empty after `v=spf1` / cannot be tokenized into terms. It tolerates unknown-but-harmless terms rather than failing them, to avoid false FAILs on valid-but-exotic records. Full RFC 7208 ABNF conformance (macro `%{}` syntax, CIDR-length bounds) is **not** attempted — those only matter for evaluation, which v1 does not do.

**Rationale**: Bounds the pure parser to the failure modes operators actually hit. The boundary is documented and table-tested so `/speckit.analyze` and review can confirm the tests match it.

**Alternatives considered**: Strict ABNF (high effort, false-FAIL risk) or delegating to a library (unnecessary without the budget check) — both rejected.

---

## Resolved unknowns summary

| Unknown | Resolution |
|---------|-----------|
| Where to query SPF/DMARC records | Authoritative NS, TypeTXT, reuse `ExchangeWithRetry` (R-1) |
| Multi-string / multi-RR handling | Concatenate within RR, select RRs by version prefix (R-2) |
| SPF parsing | **Pure hand-rolled parser** — presence, multiple-record count, syntactic terminal qualifier; no DNS, no recursion, no dependency (R-3, R-9) |
| SPF DNS-lookup budget | **Deferred to #58** — the only resolution-requiring check; out of v1 scope (R-4) |
| DMARC parse depth | Hand-parsed `tag=value;` — p=/sp=/rua/ruf; NXDOMAIN=NODATA=absent (R-5) |
| Metric shape | Per-zone gauges + info-gauges, zero-emission booleans (R-6) |
| Dashboard | 4-row "Email auth — status" panel, four-state, thresholds in PromQL (R-7) |
| Demo zones | Six static-TXT zones, fully offline (R-8) |
| New dependency | **None** — v1 is pure string parsing (R-3) |
