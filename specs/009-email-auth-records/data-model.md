# Phase 1 Data Model: Email-Authentication DNS Records

The exporter is stateless within a probe cycle; these are in-memory parse results that the `email_auth` prober produces and the registry turns into gauges. No persistence.

## Entity: SPFRecord

Parsed result of the apex TXT query for one zone.

| Field | Type | Notes |
|-------|------|-------|
| `Present` | bool | at least one `v=spf1` RR found at apex |
| `RecordCount` | int | number of distinct `v=spf1` RRs (>1 ⇒ RFC 7208 §3.2 PermError) |
| `Raw` | string | the concatenated single record (empty if absent or multiple) |
| `Valid` | bool | parsed without malformation (false when absent, multiple, or unparseable) |
| `TerminalQualifier` | enum: `fail`/`softfail`/`neutral`/`pass`/`none` | syntactic read of the last `all` term; `none` when no `all` term present |

*(The DNS-lookup-budget fields — count / exceeded / eval-complete — belong to the deferred [#58](https://github.com/SJrX/dnshealth_exporter/issues/58) and are not part of v1.)*

**Source / derivation rules** (all from pure parsing of the apex TXT answer; R-2/R-3/R-9):
- `Present` and `RecordCount` ← count of `v=spf1` RRs (concatenated per RR, selected by version prefix). `> 1` ⇒ the §3.2 multiple-record PermError.
- `TerminalQualifier` ← the last whitespace term matching `[-~?+]?all`, qualifier mapped `-→fail`/`~→softfail`/`?→neutral`/`+ or none→pass`; no such term ⇒ `none`. Syntactic only — no `redirect` following in v1.
- `Valid = false` when `RecordCount > 1` (multiple records — `Raw = ""`, can't pick one) **or** the single record is empty-after-`v=spf1` / untokenizable (R-9). Dashboard row A FAILs in both.
- `Present == false` ⇒ qualifier field not meaningful; the prober emits `spf_present 0` and suppresses the info gauge (dashboard rows read N/A via `present==0`).

## Entity: DMARCRecord

Parsed result of the `_dmarc.<zone>` TXT query for one zone.

| Field | Type | Notes |
|-------|------|-------|
| `Present` | bool | a `v=DMARC1` RR found at `_dmarc.<zone>` (NXDOMAIN and NODATA both ⇒ false) |
| `Raw` | string | the concatenated record (empty if absent) |
| `Valid` | bool | present and carries a valid `p=` tag (RFC 7489 §6.3) |
| `Policy` | enum: `none`/`quarantine`/`reject`/`""` | the `p=` value; empty when absent or malformed |
| `SubdomainPolicy` | enum + empty | the `sp=` value if present (optional, FR-010) |
| `RUAPresent` | bool | a `rua=` tag is present (optional) |
| `RUFPresent` | bool | a `ruf=` tag is present (optional) |

**Validation / derivation rules**
- `Present == true && Policy == ""` ⇒ `Valid = false` (malformed; dashboard row D FAILs).
- `Present == false` ⇒ row D WARN (absent), row E N/A.

## Entity: EmailAuthResult (per zone)

The bundle the prober assembles per zone and converts into `ProbeResult`s (the existing data-only shape: `Zone`, `Check:"email_auth"`, `Metrics map[string]float64`, `Labels map[string]string`).

```
EmailAuthResult
├── Zone   string
├── SPF    SPFRecord
└── DMARC  DMARCRecord
```

### Mapping to metrics (see contracts/email-auth-metrics.md)

| Field | Metric |
|-------|--------|
| `SPF.Present` | `dnshealth_spf_present` |
| `SPF.RecordCount` | `dnshealth_spf_record_count` |
| `SPF.Valid` | `dnshealth_spf_valid` |
| `SPF.TerminalQualifier` | `dnshealth_spf_terminal_all{qualifier=…}` (info) |
| `DMARC.Present` | `dnshealth_dmarc_present` |
| `DMARC.Valid` | `dnshealth_dmarc_valid` |
| `DMARC.Policy` | `dnshealth_dmarc_policy{policy=…}` (info) |
| `DMARC.SubdomainPolicy` | `dnshealth_dmarc_sp_policy{policy=…}` (info, optional) |
| `DMARC.RUAPresent` / `RUFPresent` | `dnshealth_dmarc_rua_present` / `_ruf_present` (optional) |

## Lifecycle (per probe cycle)

1. Runner resolves the zone's authoritative nameservers (existing step) and invokes `email_auth` like every other prober.
2. `email_auth` issues TypeTXT at apex → `spf.go` pure-parses into `SPFRecord` (count `v=spf1` RRs, read terminal qualifier). No further DNS.
3. `email_auth` issues TypeTXT at `_dmarc.<zone>` → parses into `DMARCRecord`.
4. Assembles `EmailAuthResult` → emits `ProbeResult`s with the metrics/labels above.
5. Registry builds gauges; boolean per-zone gauges use Reset+Set(0) zero-emission so absent records read `0`, not missing.
6. Dashboard PromQL applies the four-state severity thresholds (no verdict logic in the exporter).

## Out of model (Tier 1)

- DKIM key records (→ #57), MTA-STS / TLS-RPT / BIMI / DANE (Tier 2/3).
- Per-authoritative-server divergence of SPF/DMARC (rare; deferred — R-1).
- DMARC alignment modes (`adkim`/`aspf`), `pct`, report-interval; SPF macro (`%{}`) validation; `rua` external-domain authorization.
