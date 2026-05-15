.PHONY: build test test-integration vet fmt clean demo-smoke

BINARY := dnshealth_exporter
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
REVISION := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X github.com/prometheus/common/version.Version=$(VERSION) \
           -X github.com/prometheus/common/version.Revision=$(REVISION) \
           -X github.com/prometheus/common/version.Branch=$(BRANCH) \
           -X github.com/prometheus/common/version.BuildDate=$(BUILD_DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go test ./...

test-integration:
	go test -tags=integration -count=1 -v ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

clean:
	rm -f $(BINARY)

# Demo smoke test — brings the demo stack up, asserts expected /metrics
# series for healthy and broken zones, then tears down. Requires Docker.
# Standalone target — not invoked by `make` or `make test`.
demo-smoke:
	cd demo && ./smoke.sh
