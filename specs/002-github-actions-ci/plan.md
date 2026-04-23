# Implementation Plan: GitHub Actions CI Pipeline

**Branch**: `002-github-actions-ci` | **Date**: 2026-04-22 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `specs/002-github-actions-ci/spec.md`

## Summary

Add two GitHub Actions workflows: a CI workflow that runs tests and
vet on every PR and push to main, and a release workflow that uses
GoReleaser to build and publish binaries when a semver tag is pushed.

## Technical Context

**Language/Version**: Go 1.26.x
**CI Platform**: GitHub Actions
**Release Tool**: GoReleaser
**Target Platforms**: Linux (amd64, arm64), macOS (amd64, arm64), Windows (amd64, arm64)
**Project Type**: Configuration files only (no Go code changes)

## Constitution Check

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Robust Integration Testing | PASS | CI runs integration tests on every PR. |
| II. Prometheus Naming Conventions | N/A | No metric changes. |
| III. Modern Go Ecosystem | PASS | Go 1.26.x in CI. |
| IV. Structured Logging | N/A | No logging changes. |
| V. Zone-Focused Detection Scope | N/A | No exporter changes. |
| VI. Prometheus Ecosystem Conventions | PASS | GoReleaser follows Prometheus release patterns. |
| VII. Well-Behaved Binary | PASS | GoReleaser injects version/revision via ldflags. |

All gates pass.

## Project Structure

### New Files

```text
.
├── .github/
│   └── workflows/
│       ├── ci.yml              # Test + vet on PR and main push
│       └── release.yml         # GoReleaser on semver tag push
└── .goreleaser.yml             # GoReleaser configuration
```

No existing files modified (except CLAUDE.md plan pointer).

## CI Workflow Design

**Trigger**: `push` to main, `pull_request` targeting main.

**Steps**:
1. Checkout code
2. Set up Go 1.26.x
3. Cache Go modules
4. Run `go vet ./...`
5. Run `go test ./...` (unit tests)
6. Run `go test -tags=integration -count=1 ./...` (integration tests)

## Release Workflow Design

**Trigger**: `push` of tags matching `v*.*.*`.

**Steps**:
1. Checkout code with full history (GoReleaser needs tags)
2. Set up Go 1.26.x
3. Run tests (gate — don't release broken code)
4. Run GoReleaser with `goreleaser release --clean`

**GoReleaser config**: Build for Linux, macOS, and Windows
(amd64 + arm64 each), generate checksums, create GitHub Release
with changelog from commits. Follows Prometheus exporter release
conventions.

## Complexity Tracking

No Constitution Check violations. Table not needed.
