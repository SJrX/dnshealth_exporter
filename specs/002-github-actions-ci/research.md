# Research: GitHub Actions CI Pipeline

**Date**: 2026-04-22
**Feature**: GitHub Actions CI (`002-github-actions-ci`)

## Technology Decisions

### GoReleaser

- **Decision**: Use GoReleaser for release automation
- **Rationale**: Standard tool for Go projects. Used by Prometheus
  exporters (blackbox_exporter, node_exporter). Handles
  cross-compilation, checksums, changelogs, and GitHub Release
  creation from a single config file.
- **Alternatives**: Manual `go build` + `gh release create`
  (more work, less reproducible); goreleaser-action for GitHub
  Actions integration (use the official action).

### GitHub Actions Go Setup

- **Decision**: Use `actions/setup-go` with module caching
- **Rationale**: Official action, supports caching Go modules
  via `cache: true` parameter. Eliminates need for separate
  cache action.

### Workflow Split

- **Decision**: Two separate workflow files (ci.yml, release.yml)
- **Rationale**: CI runs on every PR/push; release only on tags.
  Separate files keep triggers clean and avoid conditional logic.
  CI failure blocks PRs immediately; release failure is a
  separate concern.
