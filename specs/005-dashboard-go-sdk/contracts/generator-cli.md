# Contract: Dashboard Generator CLI

**Date**: 2026-05-15
**Type**: Internal CLI (developer tool, not a runtime service)

The dashboard generator is a Go program invoked by maintainers. It
takes no flags, reads no environment variables, and writes a fixed
set of files relative to the current working directory.

## Invocation

```text
go run ./demo/dashboard
```

Or via the Makefile target:

```text
make dashboards
```

Both forms MUST be invoked from the repository root.

## Pre-conditions

- Go toolchain installed (version per `go.mod`).
- Repository root as the working directory.
- Network access to `proxy.golang.org` (or pre-populated module cache)
  for the first invocation only.

## Outputs

On success, the generator writes:

| Path                                                          | Content                                |
|---------------------------------------------------------------|----------------------------------------|
| `demo/grafana/dashboards/dnshealth-overview.json`             | Full dashboard (with info-text panel)  |
| `demo/grafana/dashboards/dnshealth-overview-clean.json`       | Same dashboard minus the info-text panel |

Both files are pretty-printed JSON (`MarshalIndent` with 2-space
indent) terminated by a single trailing newline.

## Exit codes

| Code | Meaning                                                         |
|------|-----------------------------------------------------------------|
| 0    | Both files written successfully.                                |
| 1    | Builder.Build() returned an error, or os.WriteFile failed. The error is printed to stderr. |

## Determinism contract

For a given commit (Go toolchain version, SDK pseudo-version pinned in
`go.sum`, generator source unchanged), running the generator twice in
a row MUST produce byte-identical output for both files. The drift
test in `demo/dashboard/dashboard_test.go` enforces this.

## Side effects

- Overwrites the two output files in place.
- Does NOT modify any other files.
- Does NOT invoke `git`.
- Does NOT make network calls (after module cache is populated).
