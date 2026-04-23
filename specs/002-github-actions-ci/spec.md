# Feature Specification: GitHub Actions CI Pipeline

**Feature Branch**: `002-github-actions-ci`
**Created**: 2026-04-22
**Status**: Draft
**Input**: User description: "add GitHub Actions CI Pipeline with GoReleaser for releases"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Developer Opens a Pull Request (Priority: P1)

A developer pushes a branch and opens a pull request. GitHub Actions automatically runs the full test suite (unit tests and integration tests) and reports the results on the PR. The developer sees pass/fail status before merging.

**Why this priority**: This is the core value — automated quality gates on every PR prevent broken code from reaching main.

**Independent Test**: Open a PR with passing tests, verify the check passes. Open a PR with a failing test, verify the check fails and blocks merge.

**Acceptance Scenarios**:

1. **Given** a developer pushes a branch with passing code, **When** they open a PR, **Then** the CI pipeline runs unit tests and integration tests and reports a green check.
2. **Given** a developer pushes code with a failing test, **When** the CI pipeline runs, **Then** it reports a red check with clear failure output.
3. **Given** the CI pipeline is running, **When** the developer pushes additional commits to the same PR, **Then** the pipeline re-runs automatically.

---

### User Story 2 - Maintainer Merges with Confidence (Priority: P2)

A maintainer reviews a PR and sees that CI has passed. They can merge knowing that unit tests, integration tests, and code quality checks have all succeeded.

**Why this priority**: Without CI status on PRs, maintainers must run tests locally before merging — slow and error-prone.

**Independent Test**: Configure branch protection to require CI to pass. Attempt to merge a PR with failing CI. Verify it is blocked.

**Acceptance Scenarios**:

1. **Given** a PR with passing CI, **When** the maintainer clicks merge, **Then** the merge succeeds.
2. **Given** a PR with failing CI, **When** the maintainer attempts to merge, **Then** the merge is blocked (assuming branch protection is configured).

---

### User Story 3 - Maintainer Creates a Release (Priority: P3)

A maintainer tags a version on main. GitHub Actions automatically builds release binaries for multiple platforms and publishes them as a GitHub Release with checksums and changelogs.

**Why this priority**: Automated releases eliminate manual build/upload steps and ensure every release is reproducible and consistent.

**Independent Test**: Push a tag like `v0.1.0` to main. Verify a GitHub Release is created with binaries for Linux (amd64, arm64) and a checksum file.

**Acceptance Scenarios**:

1. **Given** a maintainer pushes a semver tag (e.g., `v0.1.0`), **When** the release workflow runs, **Then** binaries are built for Linux, macOS, and Windows (amd64 + arm64 each) and published as a GitHub Release.
2. **Given** a release is published, **When** a user downloads a binary, **Then** a SHA256 checksum file is available to verify the download.
3. **Given** a tag is pushed that is NOT a semver tag, **When** GitHub Actions evaluates, **Then** the release workflow does NOT run.

### Edge Cases

- What happens when the CI runner doesn't have network access to reach DNS root servers? (Integration tests use in-process DNS servers, so this should not be an issue.)
- What happens when a dependency fails to download during CI?
- What happens when multiple PRs run CI simultaneously?
- What happens if a tag is pushed but CI tests fail? (Release should not publish broken binaries.)

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: CI MUST run on every push to a pull request targeting main.
- **FR-002**: CI MUST run unit tests (`go test ./...`).
- **FR-003**: CI MUST run integration tests (`go test -tags=integration ./...`).
- **FR-004**: CI MUST run `go vet ./...` to catch code quality issues.
- **FR-005**: CI MUST report pass/fail status as a GitHub check on the PR.
- **FR-006**: CI MUST cache Go modules to speed up subsequent runs.
- **FR-007**: CI MUST run on pushes to main (post-merge validation).
- **FR-008**: A release workflow MUST trigger when a semver tag (e.g., `v0.1.0`) is pushed.
- **FR-009**: The release workflow MUST build binaries for Linux (amd64, arm64), macOS (amd64, arm64), and Windows (amd64, arm64) using GoReleaser.
- **FR-010**: The release workflow MUST publish binaries, checksums, and a changelog as a GitHub Release.
- **FR-011**: The release workflow MUST NOT publish if tests fail.
- **FR-012**: Dependabot MUST be configured to automatically create PRs for Go module and GitHub Actions dependency updates on a weekly schedule.
- **FR-013**: Release binaries MUST have version, revision, branch, and build date injected at build time. The same version metadata MUST be available via `--version` flag and `dnshealth_build_info` metric.
- **FR-014**: Local builds via `make build` MUST inject the same version metadata as release builds.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: CI pipeline completes (unit + integration + vet) in under 5 minutes.
- **SC-002**: Every PR to main has an automated CI check visible before merge.
- **SC-003**: Integration tests pass in CI without any external dependencies beyond the Go toolchain.
- **SC-004**: A tagged release produces downloadable binaries on GitHub within 10 minutes of the tag push.

## Assumptions

- GitHub Actions is the CI platform (repository is hosted on GitHub).
- Integration tests use in-process `miekg/dns` servers and require no Docker, no external DNS access, and no special permissions.
- Two workflow files: one for CI (test/vet on PRs and main), one for release (GoReleaser on tags).
- Branch protection rules are configured manually by the maintainer (out of scope for this feature, but the CI check must be available for branch protection to reference).
- Go module caching uses GitHub Actions' built-in cache mechanism.
- The project uses a single Go version (1.26.x); matrix testing across multiple versions is out of scope.
- GoReleaser manages cross-compilation, checksums, and changelog generation. A `.goreleaser.yml` config is committed to the repository.
- Release binaries target Linux, macOS, and Windows (amd64 + arm64 each), following the pattern of official Prometheus exporters.
- Docker image publishing is out of scope for this feature.
