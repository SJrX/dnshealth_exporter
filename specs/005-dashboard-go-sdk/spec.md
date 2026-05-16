# Feature Specification: Port demo dashboard to a typed dashboard SDK + port-mapping tweaks

**Feature Branch**: `005-dashboard-go-sdk`
**Created**: 2026-05-15
**Status**: Draft
**Input**: User description: "I want to port the demo dashboard implemented in 004 from raw JSON to the Grafana Foundation SDK, using Go, also a few tweaks to ports."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Edit the demo dashboard as code, not as a 600-line JSON blob (Priority: P1)

A maintainer wants to change a panel — add a new column to the SOA records
table, retitle a panel, or rename a metric reference — and have the change
appear as a small, reviewable diff in source control. Today, the dashboard
is a hand-written Grafana JSON file: even cosmetic tweaks produce noisy
diffs (panel `id` renumbering, key reorderings, deeply nested transformation
arrays), and metric/label typos are only caught when Grafana renders the
dashboard at runtime.

**Why this priority**: This is the entire reason for the port. If editing
the dashboard isn't materially nicer after the port, the port wasn't
worth doing.

**Independent Test**: A reviewer is given two changes — "rename the
'NS records — from the zone' panel to 'NS records — authoritative'" and
"add a recursion-available column to the parent NS table" — and asked
to make them. The diffs in the resulting pull request are confined to the
panel the reviewer touched, are under ~30 lines each, and do not include
unrelated key-order or `id` churn elsewhere in the dashboard.

**Acceptance Scenarios**:

1. **Given** a clean checkout, **When** a maintainer edits the typed
   dashboard source to rename a panel and regenerates the JSON, **Then**
   the resulting diff against the previous JSON is limited to the
   renamed panel's title field (no spurious churn elsewhere).
2. **Given** the typed dashboard source references a metric name,
   **When** the metric name is misspelled, **Then** the typo surfaces
   at build time (compile error or build-time validation), not when
   Grafana renders the dashboard.
3. **Given** the regenerated dashboard JSON, **When** the demo stack
   is started and Grafana loads it via provisioning, **Then** every
   panel from the v1 dashboard renders with equivalent data.

---

### User Story 2 - Regenerate the dashboard with one documented command (Priority: P1)

A contributor who has just edited the typed dashboard source wants a
single, discoverable command to regenerate the JSON file that Grafana
provisions from. They should not have to remember an ad-hoc invocation
or stitch together multiple steps.

**Why this priority**: A typed source-of-truth that nobody knows how to
regenerate quickly devolves into the JSON drifting out of sync with the
typed source, which is a worse state than today.

**Independent Test**: A first-time contributor reads the demo README,
finds the regeneration command in under one minute, runs it, and
confirms the dashboard JSON is updated.

**Acceptance Scenarios**:

1. **Given** a checkout with the typed dashboard source modified,
   **When** the contributor runs the documented regeneration command,
   **Then** `demo/grafana/dashboards/dnshealth-overview.json` is
   regenerated from the typed source.
2. **Given** the typed dashboard source is unchanged, **When** the
   regeneration command is run, **Then** the resulting JSON file
   matches the previously committed JSON byte-for-byte (deterministic
   output).
3. **Given** a checkout, **When** the regeneration command is run on
   a stock developer machine with the project's documented toolchain
   installed, **Then** it succeeds without requiring any additional
   per-developer setup.

---

### User Story 3 - Switch the exporter to a DNS-themed default port; keep all ports overridable (Priority: P2)

An operator running the demo on a workstation that already has the
production `dnshealth_exporter` bound to its current default port
(`9266`) wants the demo's exporter to come up on a DNS-themed,
prober-conventional port (`9053`) instead, and wants every demo
service's host port to remain overridable via a documented
environment variable.

**Why this priority**: P2 because the existing
`${GRAFANA_PORT:-3000}`-style overrides already work; the change is
the new exporter default and the documentation pass that goes with it.

**Independent Test**: An operator with `dnshealth_exporter` already
bound to `9266` on the host runs the documented demo start command and
the demo exporter comes up on `9053` without conflict. The README's
URLs reference `9053`. Setting `EXPORTER_PORT=9999` and restarting
binds the demo exporter to `9999` instead.

**Acceptance Scenarios**:

1. **Given** the host already has another `dnshealth_exporter`
   instance bound to `9266`, **When** the operator runs the
   documented demo start command, **Then** the demo's exporter
   binds to `9053` without conflict.
2. **Given** the demo is running on the new defaults, **When** the
   operator opens the demo README, **Then** every URL printed in the
   README references `9053` for the exporter (and `3000` / `9090` for
   Grafana / Prometheus).
3. **Given** an operator wants to override any of the demo's host
   ports, **When** they set the documented environment variable
   (`EXPORTER_PORT`, `GRAFANA_PORT`, or `PROMETHEUS_PORT`) and
   restart the stack, **Then** the override is respected.
4. **Given** the operator picks a port that is already in use,
   **When** the demo starts, **Then** the failing service exits
   with a clear log line identifying the bind error (no panic, no
   silent partial start).

---

### Edge Cases

- **SDK can't represent an existing panel feature**: If the typed SDK
  cannot express something the v1 JSON dashboard does (specific
  transformation, value-mapping option, panel option), that gap MUST
  be surfaced clearly during the port (issue, comment, or escape hatch
  to drop in raw JSON for that one panel) rather than silently
  dropping the feature.
- **Generated JSON drift across SDK/library upgrades**: If the SDK or
  its underlying schema is upgraded, the regenerated JSON may differ
  cosmetically from the committed file. The regeneration step MUST
  produce stable output for a given SDK version, and any churn from a
  version bump MUST be visible as a single committed regeneration
  (not silent runtime divergence).
- **Operators without the language toolchain**: An operator who only
  wants to *run* the demo (not edit the dashboard) MUST NOT need to
  install the dashboard SDK's toolchain. The committed JSON is the
  artifact provisioned by Grafana.
- **Smoke test sensitivity**: The existing demo smoke test asserts on
  metric series, not on dashboard structure. The port MUST keep that
  test passing without modification.
- **Port conflicts after the tweak**: If `9053`, `3000`, or `9090`
  collides on a given host, the failure mode MUST be a clear Docker
  / exporter error identifying the conflicting port, plus README
  guidance on overriding via the env vars listed in FR-014.

## Requirements *(mandatory)*

### Functional Requirements

#### Typed dashboard source

- **FR-001**: The demo dashboard MUST be expressed as typed source code
  (using the Grafana Foundation SDK in Go, per user direction) that
  generates the Grafana-importable JSON.
- **FR-002**: The typed source MUST live under the demo directory
  (e.g., `demo/dashboard/`) and MUST NOT introduce new dependencies on
  the dashboard-generation code from the production exporter binary.
- **FR-003**: Regenerating the dashboard MUST be a single, documented
  command (e.g., `go run ./demo/dashboard` or a Makefile target).
- **FR-004**: The regeneration step MUST be deterministic: identical
  source MUST produce a byte-identical JSON file across runs on the
  same toolchain version.
- **FR-005**: The generated JSON files MUST be written to
  `demo/grafana/dashboards/dnshealth-overview.json` (full variant)
  and `demo/grafana/dashboards/dnshealth-overview-clean.json` (no-
  info-text variant). Grafana provisioning is configured to load
  every JSON file in that directory, so both variants appear as
  separate dashboards in the demo Grafana without provisioning
  changes (provisioning config update may still be needed if it
  references a single file by name).
- **FR-006**: The committed JSON MUST be the artifact of record:
  operators running the demo MUST NOT need to install the dashboard
  SDK toolchain to bring the stack up.
- **FR-007**: A CI check or documented contributor step MUST exist to
  detect when the committed JSON has drifted from the typed source
  (i.e., someone edited the JSON by hand or forgot to regenerate
  after editing the source).

#### Dashboard parity

- **FR-008**: The typed source MUST emit **two** dashboard variants
  from the same definition:
  - `dnshealth-overview.json` — the full demo dashboard, including
    every panel currently in the v1 dashboard (markdown header, the
    per-category status tables (Parent / NS / SOA), the records
    tables (NS records from parent, NS records from zone, SOA
    serials), and the operator timeseries panels collapsed by
    default).
  - `dnshealth-overview-clean.json` — the same dashboard with the
    markdown header (info text) panel removed, suitable for users
    who want to embed the dashboard in their own context without the
    demo-specific narration.
  Both variants MUST be generated from the same shared panel
  definitions (no copy-paste); the only difference is the presence
  or absence of the markdown info panel and any layout reflow that
  removal entails.
- **FR-009**: The generated dashboard MUST preserve the `$zone`
  templating variable, with the same default value (`healthy.demo.`)
  and the same query-driven option list.
- **FR-010**: All panel queries MUST reference the same metric names,
  labels, and filters used by the v1 JSON dashboard, so existing
  Prometheus rules and the smoke test remain valid.
- **FR-011**: Visual parity is the goal for v1 — the generated
  dashboard MUST look and behave like the v1 dashboard from a viewer's
  perspective. Cosmetic improvements are explicitly out of scope for
  this port.

#### Port-mapping tweaks

- **FR-012**: The demo's exporter MUST bind to host port `9053` by
  default (chosen because `53` is the well-known DNS port and `9053`
  sits in the conventional Prometheus exporter `9xxx` range — making
  the demo exporter visibly DNS-themed and unlikely to collide with
  another `dnshealth_exporter` running on its production default
  `9266`).
- **FR-013**: Grafana and Prometheus MUST keep their well-known host
  port defaults (`3000` and `9090` respectively). Operators who
  collide with locally-installed Grafana or Prometheus instances
  override via env var (FR-014).
- **FR-014**: Every demo service's host port MUST remain overridable
  via a documented environment variable, following the existing
  `${GRAFANA_PORT:-...}` pattern in `demo/docker-compose.yml`. The
  set is: `EXPORTER_PORT` (default `9053`), `GRAFANA_PORT` (default
  `3000`), `PROMETHEUS_PORT` (default `9090`).
- **FR-015**: The demo README MUST reference the new exporter
  default (`9053`) in every URL/example it documents, and MUST list
  the three override env vars together with their defaults.

#### Validation & test impact

- **FR-016**: The existing demo smoke test (`demo/smoke.sh`) MUST
  continue to pass without modifications to its assertions.
- **FR-017**: If the smoke test references hard-coded host ports, it
  MUST be updated to use the new defaults (or to read them from the
  same environment variables as compose).

### Key Entities

- **Typed dashboard definition**: Go source code that constructs the
  dashboard, its variables, panels, queries, and transformations using
  the Grafana Foundation SDK.
- **Generated dashboard JSON**: The Grafana-importable artifact
  produced by running the typed dashboard generator. Lives at the
  same path Grafana provisioning currently reads from.
- **Port-mapping configuration**: The host-to-container port bindings
  declared in `demo/docker-compose.yml`, parameterised via
  environment variables.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A panel rename (single field change) produces a diff
  in the typed source under 5 lines. The corresponding regenerated
  JSON diff is similarly localised — no churn outside the affected
  panel block.
- **SC-002**: A misspelled metric name or label in the typed source
  is caught at build time (build fails / generation fails), not at
  Grafana render time.
- **SC-003**: A first-time contributor can find and run the
  dashboard regeneration command from the demo README in under
  2 minutes.
- **SC-004**: The demo smoke test (`demo/smoke.sh`) passes against
  the regenerated dashboard with no assertion changes.
- **SC-005**: An operator with another `dnshealth_exporter` already
  bound to its production default port (`9266`) on the host can
  bring up the demo using the documented start command without an
  exporter port conflict, because the demo exporter defaults to
  `9053`.
- **SC-007**: An operator can find the override env-var name for any
  of the three demo services in the demo README in under 60 seconds.
- **SC-006**: Regenerating the dashboards twice in a row, with no
  source changes between runs, produces byte-identical JSON files
  both times (for both variants).
- **SC-008**: A user who only wants the dashboard without the demo
  narration imports `dnshealth-overview-clean.json` into their own
  Grafana and sees identical panel behaviour to the demo Grafana's
  full dashboard, minus the info-text panel.

## Assumptions

- The Grafana Foundation SDK in Go is mature enough to express every
  panel type, transformation, query, and template variable currently
  used in the v1 dashboard. Any gaps surface as visible work during
  the port (not silent feature loss).
- The committed JSON file remains the source Grafana provisions from;
  the typed source generates that file on demand, and operators do
  not need a Go toolchain to run the demo.
- Visual parity with the v1 dashboard is the success bar; this port
  is not the place to redesign panels.
- Port-mapping tweaks apply to host-side bindings only; container
  internal ports stay the same.
- The exporter exits with code 1 and a clear log line on bind
  failure (verified in `main.go:170` — `web.ListenAndServe` returns
  the bind error, which is logged and propagated). No panic, no
  silent partial start. The same is true of Prometheus and Grafana
  containers, where Docker reports the bind failure.
- The Grafana version targeted is the same one the demo currently
  pins (no Grafana version bump as part of this work).
- The existing demo zones, exporter config, CoreDNS configuration,
  and smoke test stay as-is. This feature changes the *dashboard
  authoring* and the *port mappings*; nothing else in the demo
  changes.
- No new exporter metrics are introduced by this work — the typed
  dashboard references the same metric/label set the v1 dashboard
  already uses.
