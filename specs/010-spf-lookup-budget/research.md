# Phase 0 Research: SPF DNS-Lookup Budget Check

All clarifications were resolved in `/speckit.clarify` (Session 2026-06-01): hand-roll/no-dep, void-limit out of scope, stop-at-11. These decisions are the inputs; the items below work out the algorithm and the resolver shape.

---

## R-1 — Counting needs to resolve ONLY `include`/`redirect` targets

**Decision**: The recursive walk fetches DNS records **only** for `include` and `redirect` targets. The other four lookup-incurring mechanisms — `a`, `mx`, `ptr`, `exists` — are counted **syntactically** (`+1` each) with **zero DNS queries**.

**Rationale**: RFC 7208 §4.6.4 counts *terms that cause a DNS query*. Only `include` and `redirect` pull in *another SPF record* whose own terms must then be counted recursively; `a`/`mx`/`ptr`/`exists` each cost exactly one lookup and pull in nothing further. To **count** the budget we need the *number* of those terms, not their resolved values — so we never query them. This shrinks the new "resolver" surface from "resolve arbitrary A/MX/PTR/TXT for any name" to "fetch the SPF TXT of an `include`/`redirect` target," and it means graceful-degradation (R-3) only ever concerns include/redirect targets.

**Alternatives considered**: Actually resolving `a`/`mx`/etc. — pointless for counting and would multiply DNS traffic and failure surface.

## R-2 — The counting algorithm

**Decision**: A bounded recursive descent over the parsed mechanism list:

```
countSPFLookups(record, fetch, visited, depth) -> (count, complete):
  complete = true
  hasAll = record contains an `all` mechanism
  for term in record.terms:
    if term in {a, mx, ptr, exists}:        count += 1
    elif term is include(target):           count += 1; recurse(target)
    elif term is redirect(target) AND NOT hasAll:  count += 1; recurse(target)
    # all other terms (ip4, ip6, all, unknown) cost 0 lookups
    if count > 10: return (11, complete)     # stop-at-11 (clarification Q3)

  recurse(target):
    if target has a macro %{...}:  complete = false; return     # unresolvable (assumption)
    if target in visited OR depth >= MAX_DEPTH:  return over-budget   # cycle/depth (R-5)
    rec, ok = fetch(target)
    if not ok:  complete = false; return                        # unreachable (R-3)
    sub_count, sub_complete = countSPFLookups(rec, fetch, visited+target, depth+1)
    count += sub_count;  complete = complete AND sub_complete
    if count > 10: return (11, complete)
```

**Rationale**: Directly encodes FR-001/FR-002/FR-003. `count` is checked after every increment so the walk stops the instant it knows the record is over budget (stop-at-11, Q3). `MAX_DEPTH` (e.g. 20 — well past any legitimate SPF nesting) plus the `visited` set plus stop-at-11 together make it impossible to hang.

**Alternatives considered**: Counting without recursion (undercounts — the whole point is nested includes); full SPF evaluation (out of scope, spec).

## R-3 — Graceful degradation (US2 / FR-004)

**Decision**: An `include`/`redirect` target that fails to resolve (timeout, SERVFAIL, NXDOMAIN→no SPF record, or a macro target) sets `complete = false` and contributes **only its own `+1`** (the include term itself counted) — it does **not** add a sub-count. Critically: **`budget_exceeded` is asserted (1) only when the count genuinely exceeds 10 among the lookups that resolved.** When `complete == false` and the count is ≤10, the dashboard row does NOT FAIL — an unreachable third-party include this cycle can't manufacture a false over-budget verdict. When `complete == false` AND the resolved count already exceeds 10, it's genuinely over budget regardless, so FAIL stands.

**Rationale**: This is what makes the check trustworthy rather than a transient-include false-alarm generator (US2). `eval_complete=0` is the honest caveat surfaced in the row's detail text.

**Alternatives considered**: Treating an unreachable include as 0 lookups (hides real cost) or as over-budget (false FAIL) — both rejected.

## R-4 — The iterative-from-root resolver (`resolveSPFRecord`)

**Decision**: The production `fetch` is `resolveSPFRecord(ctx, name, client, logger) (record string, ok bool)`. It resolves the TXT records at `name` by an **iterative walk from `RootServers`**, generalizing `WalkDelegation`: walk referrals to find the authoritative server for `name`'s zone, query TXT at `name` there, concatenate each RR's strings (reusing the spec-009 multi-string logic), and return the `v=spf1` record (or `ok=false` if none / unresolved). `ok=false` on any DNS error, empty answer, or no SPF record at the target.

This keeps the demo offline: `.demo` include targets resolve from the in-stack `coredns-root` exactly as the existing delegation walk does; production uses the same `RootServers` the exporter already trusts — no external recursive resolver, no config change.

**Rationale**: Reuses trusted, tested primitives (`WalkDelegation`, `ExchangeWithRetry`, `ResolveAddress`); satisfies clarification Q1 (hand-roll, offline) and Principle VII (no new startup dependency).

**Open implementation detail (tasks)**: whether to call `WalkDelegation(name)` then a TXT query, or factor a small shared `iterativeQuery(name, qtype)` — both reuse the same loop. Sized as its own task; internal and test-covered.

## R-5 — Cyclic / self-referential includes

**Decision**: A `visited` set keyed on the (lowercased FQDN) include/redirect target prevents infinite recursion. A target already in `visited` is **not** re-walked; per spec US1.4 the zone is reported as **over budget** (a real SPF loop is a PermError, and the operator's action — fix the record — is the same as over-budget), so a detected cycle short-circuits to count 11. It is NOT surfaced as an exporter error or a hang.

**Rationale**: Matches US1.4 ("reported as exceeding the budget, not as an error") and bounds the walk.

## R-6 — `redirect` precedence

**Decision**: The `redirect=` modifier is followed (and counts) **only when the record has no `all` mechanism** (RFC 7208 §6.1). The parser exposes `hasAll`; the counter skips redirect when `hasAll` is true.

## R-7 — Metric mapping (the 3 reserved gauges now ship)

**Decision**: Per qualifying zone (single valid SPF record), emit via the existing `email_auth` ProbeResult pipeline (empty nameserver/ip labels, `by (zone)` in dashboard — same pattern as the other SPF gauges):

| Metric | Value |
|--------|-------|
| `dnshealth_spf_lookup_count{zone}` | exact count 0–10, or 11 for "≥11" (Q3) |
| `dnshealth_spf_lookup_budget_exceeded{zone}` | 1 iff count > 10 among resolved lookups (R-3), else 0 |
| `dnshealth_spf_lookup_eval_complete{zone}` | 1 if every include/redirect branch resolved, 0 if any was unreachable/macro/cycle-truncated |

Emitted **only** when SPF is present, single, and valid — so the dashboard row reads N/A (via `absent()`) for no-SPF / multiple-record / malformed zones, consistent with spec 009 row A.

## R-8 — Dashboard row (fills the spec-009 reserved slot)

**Decision**: One four-state row added to `emailAuthStatusChecks`, rendered in the SPF group (slice order: after the SPF-qualifier row, before the DMARC rows) but carrying a **fresh `refId`** so existing DMARC rows keep their `refId`s and `promql_live` pins (no renumbering). Predicate:

- **FAIL** when `dnshealth_spf_lookup_budget_exceeded == 1`.
- **PASS** when `== 0`.
- **N/A** (`naExpr`) when `absent(dnshealth_spf_lookup_budget_exceeded{zone="$zone"})` — i.e. no single valid SPF record (the gauge isn't emitted).
- No WARN state.

Detail text: names the metric, explains the §4.6.4 PermError consequence, the "≥11" stop semantics, and the `eval_complete=0` caveat (a FAIL only fires on resolved over-budget; an incomplete-eval under-budget reads PASS, not FAIL).

## R-9 — Demo over-budget zone (offline)

**Decision**: `email-toomanylookups.demo.` — apex SPF `v=spf1 include:_spf1.<zone> include:_spf2.<zone> ... -all` where each `_spfN` is a TXT record in the same zone file carrying its own `v=spf1` with a couple of `a`/`mx`/`include` terms, the whole tree summing to ≥11 lookups. All targets are `.demo` names resolvable from `coredns-root`, so the counter's `resolveSPFRecord` walks them offline and `DNSQueries`-equivalent count reaches 11. A healthy in-budget zone is the existing `email-healthy.demo.` (SPF `-all`, 0 lookups → count 0, PASS).

**Rationale**: Exercises the real recursive resolver end-to-end while staying offline (FR-008/SC-006).

## R-10 — Caching

**Decision**: For v1, the `visited` set doubles as a per-evaluation memo (a target reached twice within one zone's walk isn't re-fetched). A cross-zone, per-cycle cache of fetched SPF records (the same third-party `include:` appears across many zones) is a clean future optimization but **not** required for correctness or the demo; deferred.

**Rationale**: Keeps v1 simple; the bounded ≤11 fetches per zone are cheap enough that cross-zone caching is optional.

---

## Resolved-unknowns summary

| Unknown | Resolution |
|---------|-----------|
| Do we resolve a/mx/ptr/exists? | No — count syntactically; resolve only include/redirect (R-1) |
| Counting algorithm | Bounded recursive descent, stop-at-11, visited+depth guards (R-2) |
| Partial-failure behavior | Unreachable/macro branch ⇒ complete=false, no false over-budget (R-3) |
| Resolver shape | `resolveSPFRecord` iterative-from-root reusing WalkDelegation; offline (R-4) |
| Cyclic includes | visited-set; report over-budget per US1.4 (R-5) |
| redirect precedence | followed only when no `all` (R-6) |
| Metrics | 3 reserved gauges, emitted only for single-valid-SPF zones (R-7) |
| Dashboard | one row in SPF group, fresh refId, FAIL/PASS/N-A (R-8) |
| Demo | offline chained-include zone ≥11 (R-9) |
| Caching | per-evaluation memo only; cross-zone cache deferred (R-10) |
