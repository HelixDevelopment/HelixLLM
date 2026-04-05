.PHONY: build dev container container-push test-unit test-integration test-e2e test-race test-stress test-stress-go test-chaos test-security test-benchmark test-benchmark-go test-automation test-usecases test-all coverage probe deploy status logs monitor rebalance ingest collections stats lint fmt docs gen deps clean certs

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
	go test -v -count=1 -race -coverprofile=coverage-unit.out ./internal/...

test-integration:
	go test -v -count=1 -race ./tests/integration/

test-e2e:
	go test -v -count=1 -race -tags=e2e ./tests/integration/...

test-race:
	GOMAXPROCS=$$(nproc) go test -count=1 -race -p 1 ./internal/... ./pkg/... ./tests/...

test-stress:
	./bin/helixllm --challenges --banks-dir=challenges/banks/benchmarks/ --base-url=$${HELIX_BASE_URL:-https://localhost:8443}

test-stress-go:
	go test -v -count=1 -tags=stress -timeout=10m ./tests/stress/...

test-chaos:
	./bin/helixllm --challenges --banks-dir=challenges/banks/chaos/ --base-url=$${HELIX_BASE_URL:-https://localhost:8443}

test-security:
	./bin/helixllm --challenges --banks-dir=challenges/banks/security/ --base-url=$${HELIX_BASE_URL:-https://localhost:8443}

test-benchmark:
	./bin/helixllm --challenges --banks-dir=challenges/banks/benchmarks/ --base-url=$${HELIX_BASE_URL:-https://localhost:8443}

test-automation: build
	@echo "Running full automation pipeline..."
	$(MAKE) test-unit
	$(MAKE) test-integration
	$(MAKE) test-challenges

test-usecases:
	./bin/helixllm --challenges --banks-dir=challenges/banks/workflows/ --base-url=$${HELIX_BASE_URL:-https://localhost:8443}

test-challenges:
	./bin/helixllm --challenges --banks-dir=challenges/banks/ --base-url=https://localhost:8443

test-challenges-api:
	./bin/helixllm --challenges --banks-dir=challenges/banks/api/ --base-url=https://localhost:8443

test-all: test-unit test-integration

test-benchmark-go:
	go test -bench=. -benchmem -count=3 -run=^$$ ./internal/...

COVERAGE_THRESHOLD := 91

coverage: test-unit
	go tool cover -func=coverage-unit.out
	@echo "---"
	@TOTAL=$$(go tool cover -func=coverage-unit.out | grep '^total:' | awk '{print $$NF}' | tr -d '%'); \
	echo "Total coverage: $${TOTAL}% (threshold: $(COVERAGE_THRESHOLD)%)"; \
	if [ $$(echo "$${TOTAL} < $(COVERAGE_THRESHOLD)" | bc -l) -eq 1 ]; then \
		echo "FAIL: coverage $${TOTAL}% is below $(COVERAGE_THRESHOLD)% threshold"; \
		exit 1; \
	fi; \
	echo "PASS: coverage meets threshold"
	@echo "Full coverage report: go tool cover -html=coverage-unit.out"

# ── Cluster ──────────────────────────────────────────────
probe:
	curl -sk https://localhost:$${HELIX_PORT:-8443}/internal/cluster/probe -X POST | python3 -m json.tool

deploy:
	curl -sk https://localhost:$${HELIX_PORT:-8443}/internal/cluster/deploy -X POST | python3 -m json.tool

status:
	curl -sk https://localhost:$${HELIX_PORT:-8443}/internal/cluster/status | python3 -m json.tool

logs:
	$(CONTAINER_RUNTIME) compose -f deploy/compose.yaml logs -f

monitor:
	./bin/helixllm --monitor

rebalance:
	curl -sk https://localhost:$${HELIX_PORT:-8443}/internal/cluster/rebalance -X POST | python3 -m json.tool

# ── Knowledge ────────────────────────────────────────────
ingest:
	@test -n "$(DIR)" || (echo "Usage: make ingest DIR=./path/to/docs" && exit 1)
	@find $(DIR) -type f \( -name '*.md' -o -name '*.txt' -o -name '*.go' -o -name '*.py' \) -exec sh -c 'curl -sk https://localhost:$${HELIX_PORT:-8443}/internal/knowledge/ingest -X POST -H "Content-Type: application/json" -d "{\"title\":\"$$(basename {})\",\"content\":\"$$(cat {} | head -c 10000 | sed "s/\"/\\\\\\\"/g" | tr "\n" " ")\",\"source\":\"{}\",\"collection\":\"$${COLLECTION:-default}\"}"' \;

collections:
	curl -sk https://localhost:$${HELIX_PORT:-8443}/internal/knowledge/collections | python3 -m json.tool

stats:
	curl -sk https://localhost:$${HELIX_PORT:-8443}/internal/knowledge/stats | python3 -m json.tool

# ── Development ──────────────────────────────────────────
lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w .

docs:
	@echo "Documentation available at docs/user-guide/ and docs/manual/"
	@echo "API reference: docs/user-guide/api-reference.md"
	@ls docs/user-guide/ docs/manual/ 2>/dev/null

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
