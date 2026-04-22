# Quickstart: dnshealth_exporter

## Prerequisites

- Go 1.26+

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
  - example.com
  - example.org
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
`dnshealth_soa_*`, `dnshealth_ns_recursion_available`,
`dnshealth_ns_record`, and `dnshealth_ns_glue` metrics.

## Run Tests

Unit tests:

```bash
go test ./...
```

Integration tests (no Docker needed — uses in-process DNS servers):

```bash
go test -tags=integration ./...
```

## Shut Down

Send SIGTERM or Ctrl+C:

```bash
kill $(pgrep dnshealth_exporter)
```

The exporter shuts down gracefully.
