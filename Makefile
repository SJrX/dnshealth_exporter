.PHONY: build build-host build-arm64 test test-integration vet fmt clean demo-smoke dashboards

BINARY := dnshealth_exporter
ARM64_BINARY := $(BINARY)_linux_arm64
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
REVISION := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X github.com/prometheus/common/version.Version=$(VERSION) \
           -X github.com/prometheus/common/version.Revision=$(REVISION) \
           -X github.com/prometheus/common/version.Branch=$(BRANCH) \
           -X github.com/prometheus/common/version.BuildDate=$(BUILD_DATE)

# `make build` produces both the host binary and a statically-linked
# linux/arm64 binary — the common deploy target. Use `build-host` or
# `build-arm64` if you only want one.
build: build-host build-arm64

build-host:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

# Cross-compiled, statically linked (CGO_ENABLED=0) so it runs on any
# linux/arm64 host without a glibc dependency. Ignored by .gitignore
# via the /dnshealth_exporter_* pattern.
build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
		go build -ldflags "$(LDFLAGS)" -o $(ARM64_BINARY) .

test:
	go test ./...

test-integration:
	go test -tags=integration -count=1 -v ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

clean:
	rm -f $(BINARY) $(ARM64_BINARY)

# Demo smoke test — brings the demo stack up, asserts expected /metrics
# series for healthy and broken zones, then tears down. Requires Docker.
# Standalone target — not invoked by `make` or `make test`.
demo-smoke:
	cd demo && ./smoke.sh

# Regenerate the demo Grafana dashboard JSON files from typed Go source
# (Grafana Foundation SDK). Writes both the full and clean variants.
# See demo/dashboard/ and specs/005-dashboard-go-sdk/.
dashboards:
	go run ./demo/dashboard
