.PHONY: build dev container container-push test-unit test-integration test-e2e test-stress test-chaos test-security test-benchmark test-automation test-usecases test-all coverage probe deploy status logs monitor rebalance ingest collections stats lint fmt docs gen deps clean certs

# ── Variables ────────────────────────────────────────────
BINARY := helixllm
GOFLAGS := -ldflags="-s -w"
CONTAINER_RUNTIME := $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)
IMAGE := helixllm
TAG := dev

# ── Build ────────────────────────────────────────────────
build:
	go build $(GOFLAGS) -o bin/$(BINARY) ./cmd/helixllm

dev: certs
	HELIX_MODE=full go run ./cmd/helixllm

container:
	$(CONTAINER_RUNTIME) build -f container/Containerfile -t $(IMAGE):$(TAG) .

container-push:
	$(CONTAINER_RUNTIME) push $(IMAGE):$(TAG)

# ── Test ─────────────────────────────────────────────────
test-unit:
	go test -v -count=1 -coverprofile=coverage-unit.out ./internal/...

test-integration:
	@echo "TODO: Phase 7 — integration tests with real services"

test-e2e:
	@echo "TODO: Phase 7 — e2e tests with full cluster"

test-stress:
	@echo "TODO: Phase 7 — stress tests"

test-chaos:
	@echo "TODO: Phase 7 — chaos tests"

test-security:
	@echo "TODO: Phase 7 — security tests"

test-benchmark:
	@echo "TODO: Phase 7 — benchmark tests"

test-automation:
	@echo "TODO: Phase 7 — full automation pipeline"

test-usecases:
	@echo "TODO: Phase 7 — real-world use case validation"

test-all: test-unit test-integration test-e2e test-stress test-chaos test-security test-benchmark test-automation test-usecases

coverage: test-unit
	go tool cover -func=coverage-unit.out
	@echo "---"
	@echo "Full coverage report: go tool cover -html=coverage-unit.out"

# ── Cluster ──────────────────────────────────────────────
probe:
	@echo "TODO: Phase 6 — probe all hosts"

deploy:
	@echo "TODO: Phase 6 — deploy to cluster"

status:
	@echo "TODO: Phase 6 — cluster status"

logs:
	@echo "TODO: Phase 6 — aggregated logs"

monitor:
	@echo "TODO: Phase 6 — TUI monitor"

rebalance:
	@echo "TODO: Phase 6 — rebalance cluster"

# ── Knowledge ────────────────────────────────────────────
ingest:
	@echo "TODO: Phase 4 — ingest documents"

collections:
	@echo "TODO: Phase 4 — list collections"

stats:
	@echo "TODO: Phase 4 — knowledge base stats"

# ── Development ──────────────────────────────────────────
lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w .

docs:
	@echo "TODO: Phase 8 — generate documentation"

gen:
	go generate ./...

deps:
	git submodule update --init --recursive
	go mod tidy

clean:
	rm -rf bin/ coverage-*.out certs/

certs:
	@mkdir -p certs
	@test -f certs/cert.pem || openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
		-keyout certs/key.pem -out certs/cert.pem -days 365 -nodes \
		-subj "/CN=localhost" \
		-addext "subjectAltName=DNS:localhost,DNS:nezha.local,IP:127.0.0.1" 2>/dev/null
	@echo "TLS certs ready at certs/"
