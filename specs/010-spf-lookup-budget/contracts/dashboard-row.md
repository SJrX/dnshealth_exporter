# Contract: "SPF within the 10-lookup budget" Dashboard Row

Fills the row slot spec 009 reserved in `contracts/dashboard-panel.md` ("between rows B and C"). Added to `emailAuthStatusChecks` in `demo/dashboard/panels_status.go`, built with the existing four-state `composeStatusExpr` machinery.

## Placement & refId

- **Rendered position**: in the SPF group — after the SPF-qualifier row (B), before the DMARC rows. (Slice order controls table order.)
- **refId**: a **fresh, unused letter** (e.g. `E`), NOT inserted as `C`. This keeps the existing DMARC rows' `refId`s (`C`, `D`) and their `promql_live` pin keys (`email_auth/C/…`, `email_auth/D/…`) unchanged — no renumbering churn. The `promql_live` test addresses rows by `refId`, not position, so a non-sequential refId is purely internal.

## States

| State | Condition |
|-------|-----------|
| **FAIL** (0) | `dnshealth_spf_lookup_budget_exceeded == 1` |
| **PASS** (1) | `dnshealth_spf_lookup_budget_exceeded == 0` |
| **N/A** (2) | `absent(dnshealth_spf_lookup_budget_exceeded{zone="$zone"})` — no single valid SPF record (gauge not emitted) |

No WARN state.

## Predicate shape

Mirrors the spec-009 SPF rows (aggregate `by (zone)` over the empty-label series):

- `expr` (hard, PASS/FAIL): `(max by (zone) (dnshealth_spf_lookup_budget_exceeded{zone="$zone"}) == bool 0)` — 1 (PASS) when not exceeded, 0 (FAIL) when exceeded.
- `naExpr`: `absent(dnshealth_spf_lookup_budget_exceeded{zone="$zone"})`.
- No `warnExpr`.

(`composeStatusExpr` scalarizes each; the binary FAIL/PASS + N/A composition matches spec-009 SPF row C / DMARC row C.)

## Detail text (FR-007, guard-test enforced)

Must cover: the backing metric; that FAIL means the record exceeds the RFC 7208 §4.6.4 ten-lookup limit and receivers will PermError (SPF silently stops working); the **"≥11"** stop semantics (the count tops out at 11); and the **`eval_complete=0` caveat** — a FAIL only fires on a *resolved* over-budget count, so a transient unreachable include reads PASS, not FAIL. Investigate: expand the apex `include:` tree; the offending lookups are usually inside third-party includes.

## Validation cells (promql_live pins)

| Zone | this row |
|------|----------|
| `email-healthy.demo.` (SPF `-all`, 0 lookups) | PASS |
| `email-toomanylookups.demo.` (chained includes ≥11) | FAIL |
| `email-none.demo.` / any no-SPF zone | N/A |
| `email-broken.demo.` (multiple SPF records) | N/A |
| `email-permissive.demo.` (SPF `+all`, single valid, few lookups) | PASS |
