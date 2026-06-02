# Contract: Email-Auth Metrics

The stable external surface this feature adds to `/metrics`. All gauges, all per-zone, additive (no existing series change). Names follow constitution Principle II (`dnshealth_` prefix, snake_case, no `_total` on gauges).

## SPF

| Metric | Type | Labels | Values | Semantics |
|--------|------|--------|--------|-----------|
| `dnshealth_spf_present` | gauge | `zone` | 0 / 1 | 1 iff exactly-or-more `v=spf1` RR exists at the apex. Zero-emitted (0 when absent). |
| `dnshealth_spf_record_count` | gauge | `zone` | integer ≥ 0 | count of `v=spf1` RRs at apex. `> 1` is the RFC 7208 §3.2 permanent error. |
| `dnshealth_spf_valid` | gauge | `zone` | 0 / 1 | 1 iff exactly one SPF record that parsed without malformation. 0 when absent, multiple, or malformed. |
| `dnshealth_spf_terminal_all` | gauge (info) | `zone`, `qualifier` | always 1 | `qualifier ∈ {fail, softfail, neutral, pass, none}` — the (syntactic) terminal `all` qualifier. Emitted only for the applicable qualifier; not emitted when no SPF record. |

> The SPF DNS-lookup-budget gauges (`dnshealth_spf_lookup_count` / `_budget_exceeded` / `_eval_complete`) are **not part of v1** — they ship with the deferred lookup-budget check, [#58](https://github.com/SJrX/dnshealth_exporter/issues/58).

## DMARC

| Metric | Type | Labels | Values | Semantics |
|--------|------|--------|--------|-----------|
| `dnshealth_dmarc_present` | gauge | `zone` | 0 / 1 | 1 iff a `v=DMARC1` RR exists at `_dmarc.<zone>`. NXDOMAIN and NODATA both ⇒ 0. Zero-emitted. |
| `dnshealth_dmarc_valid` | gauge | `zone` | 0 / 1 | 1 iff present and carries a valid `p=` tag. 0 when present-but-malformed. |
| `dnshealth_dmarc_policy` | gauge (info) | `zone`, `policy` | always 1 | `policy ∈ {none, quarantine, reject}` — the `p=` value. Emitted only when a valid policy is parsed. |
| `dnshealth_dmarc_sp_policy` | gauge (info) | `zone`, `policy` | always 1 | subdomain policy from `sp=` if present (optional, FR-010). |
| `dnshealth_dmarc_rua_present` | gauge | `zone` | 0 / 1 | 1 iff a `rua=` tag is present (optional). |
| `dnshealth_dmarc_ruf_present` | gauge | `zone` | 0 / 1 | 1 iff a `ruf=` tag is present (optional). |

## Label note (implementation)

These are per-zone signals, but they are emitted through the prober's
`ProbeResult` pipeline (like `dnshealth_mx_info`), so each series also
carries empty `nameserver=""` / `ip=""` labels that `BuildRegistry`
stamps on every metric. The meaningful key is still `zone` (plus
`qualifier` / `policy` on the info gauges); the dashboard predicates wrap
selectors in `max by (zone)(…)` — the same idiom the SOA rows use over
`dnshealth_soa_*` — so the empty labels are inert. Zero-emission for the
booleans comes from the per-cycle registry plus the prober emitting a
value (0 when absent) for every reachable zone every cycle, not from a
runner-owned `Reset()+Set(0)`.

## Cardinality

Per zone: ~4 SPF series + ~6 DMARC series (info gauges contribute one series each for the single applicable enum value). For N zones, O(10·N) — bounded, low.

## Stability guarantees

- The metric **names** and the **label keys** (`zone`, `qualifier`, `policy`) are the contract. Info-gauge label **value sets** are the closed enums listed above; new values are not introduced without a spec change.
- Boolean per-zone gauges are **always present** (zero-emitted) for every configured zone, so dashboard predicates can rely on a series existing each cycle.
- The exporter emits **no PASS/FAIL verdict** — only these raw signals (Principle V).
