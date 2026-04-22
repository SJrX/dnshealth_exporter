# Quickstart: dnshealth_exporter

## Prerequisites

- Go 1.26+ installed
- Docker and Docker Compose (for integration tests)

## Build

```bash
git clone <repo-url>
cd dnshealth_exporter
go build -o dnshealth_exporter .
```

## Configure

Create `dnshealth.yml`:

```yaml
zones:
  - name: example.com
  - name: example.org
```

## Run

```bash
./dnshealth_exporter --config.file=dnshealth.yml
```

The exporter starts on `:9199` by default. Visit http://localhost:9199/metrics to see output.

## Verify

```bash
curl -s http://localhost:9199/metrics | grep dnshealth_
```

You should see `dnshealth_build_info`, `dnshealth_check_success`,
`dnshealth_soa_*` and other metrics.

## Run Tests

Unit tests (no Docker needed):

```bash
go test ./...
```

Integration tests (requires Docker):

```bash
docker compose -f testdata/docker-compose.yml up -d
go test -tags=integration ./...
docker compose -f testdata/docker-compose.yml down
```

## Shut Down

Send SIGTERM or Ctrl+C:

```bash
kill $(pgrep dnshealth_exporter)
```

The exporter shuts down gracefully.
