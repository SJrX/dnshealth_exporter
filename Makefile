.PHONY: build test test-integration vet fmt docker-up docker-down clean

BINARY := dnshealth_exporter
DOCKER_COMPOSE := docker compose -f testdata/docker-compose.yml

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

docker-up:
	$(DOCKER_COMPOSE) up -d
	@echo "Waiting for CoreDNS containers to be ready..."
	@sleep 2

docker-down:
	$(DOCKER_COMPOSE) down

clean:
	rm -f $(BINARY)
	rm -f testdata/coredns/runtime/*/zones/*.zone
