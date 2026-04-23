# Tasks: GitHub Actions CI Pipeline

**Input**: Design documents from `specs/002-github-actions-ci/`
**Prerequisites**: plan.md, spec.md, research.md

**Tests**: No test tasks — this feature is validated by pushing a PR and a tag to GitHub.

**Organization**: Tasks grouped by user story.

## Format: `[ID] [P?] [Story] Description`

---

## Phase 1: Setup

**Purpose**: Create directory structure

- [x] T001 Create `.github/workflows/` directory

---

## Phase 2: User Story 1 — Developer Opens a PR (Priority: P1)

**Goal**: CI runs unit tests, integration tests, and vet on every PR and push to main.

**Independent Test**: Open a PR, verify GitHub shows a CI check that runs tests.

- [x] T002 [US1] Create CI workflow in `.github/workflows/ci.yml` — trigger on push to main and pull_request targeting main; steps: checkout, setup Go 1.26.x with module caching, `go vet ./...`, `go test ./...`, `go test -tags=integration -count=1 ./...`

---

## Phase 3: User Story 2 — Maintainer Merges with Confidence (Priority: P2)

**Goal**: CI status is visible on PRs and can be used for branch protection.

**Independent Test**: Verify CI check appears on a PR and reports pass/fail.

No additional tasks — US1's CI workflow already provides the check. Branch protection is configured manually (out of scope).

---

## Phase 4: User Story 3 — Maintainer Creates a Release (Priority: P3)

**Goal**: Tagged releases produce binaries for Linux, macOS, and Windows via GoReleaser.

**Independent Test**: Push a `v0.1.0` tag, verify a GitHub Release is created with 6 binaries + checksums.

- [x] T003 [P] [US3] Create GoReleaser config in `.goreleaser.yml` — build for linux/darwin/windows (amd64 + arm64), generate checksums, inject version via ldflags (`-X github.com/prometheus/common/version.Version={{.Version}}` etc.), archive as `.tar.gz` for unix and `.zip` for windows
- [x] T004 [US3] Create release workflow in `.github/workflows/release.yml` — trigger on push of tags matching `v*.*.*`; steps: checkout with full history (`fetch-depth: 0`), setup Go 1.26.x, run tests (gate), run `goreleaser release --clean` with `GITHUB_TOKEN`

---

## Phase 5: Polish

- [x] T005 Verify CI workflow runs locally with `act` or by pushing a test branch (manual validation)
- [x] T006 Update `README.md` with CI badge (`![CI](https://github.com/.../.../actions/workflows/ci.yml/badge.svg)`)

---

## Dependencies & Execution Order

- **T001**: No dependencies
- **T002**: Depends on T001
- **T003, T004**: Can run in parallel, both depend on T001
- **T005, T006**: Depend on T002-T004

## Implementation Strategy

### MVP (US1 only)

1. T001 + T002 → CI runs on PRs
2. Push a test branch, open a PR, verify

### Full Feature

3. T003 + T004 → Release workflow
4. T005 + T006 → Polish
