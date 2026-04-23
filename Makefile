.PHONY: build test test-integration vet fmt clean

BINARY := dnshealth_exporter

build:
	go build -o $(BINARY) .

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
