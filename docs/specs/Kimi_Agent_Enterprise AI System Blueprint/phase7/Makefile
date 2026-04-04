# =============================================================================
# Light Local LLM System - Makefile
# =============================================================================
# Convenience commands for common operations
# =============================================================================

.PHONY: help init start stop restart status health logs clean backup update test benchmark security

# Default target
help:
	@echo "Light Local LLM System - Available Commands"
	@echo "============================================"
	@echo ""
	@echo "Deployment:"
	@echo "  make init          - Initial deployment"
	@echo "  make start         - Start all services"
	@echo "  make stop          - Stop all services"
	@echo "  make restart       - Restart all services"
	@echo "  make update        - Update services to latest"
	@echo "  make rollback      - Rollback to previous version"
	@echo ""
	@echo "Monitoring:"
	@echo "  make status        - Show service status"
	@echo "  make health        - Run health checks"
	@echo "  make logs          - View all logs"
	@echo "  make logs-rag      - View RAG service logs"
	@echo "  make logs-api      - View API Gateway logs"
	@echo ""
	@echo "Maintenance:"
	@echo "  make backup        - Create full backup"
	@echo "  make backup-clean  - Clean old backups"
	@echo "  make clean         - Clean Docker resources"
	@echo "  make prune         - Deep clean (WARNING: removes unused data)"
	@echo ""
	@echo "Testing:"
	@echo "  make test          - Run all tests"
	@echo "  make test-unit     - Run unit tests"
	@echo "  make test-int      - Run integration tests"
	@echo "  make benchmark     - Run performance benchmarks"
	@echo ""
	@echo "Security:"
	@echo "  make security      - Apply security hardening"
	@echo "  make security-check - Run security checks"
	@echo ""

# =============================================================================
# Deployment Commands
# =============================================================================

init:
	@echo "Initializing Light Local LLM System..."
	@./scripts/deploy.sh --init

start:
	@echo "Starting all services..."
	@./scripts/deploy.sh --start

stop:
	@echo "Stopping all services..."
	@./scripts/deploy.sh --stop

restart:
	@echo "Restarting all services..."
	@./scripts/deploy.sh --restart

update:
	@echo "Updating services..."
	@./scripts/deploy.sh --update

rollback:
	@echo "Rolling back to previous version..."
	@./scripts/deploy.sh --rollback

# =============================================================================
# Monitoring Commands
# =============================================================================

status:
	@./scripts/deploy.sh --status

health:
	@./scripts/deploy.sh --health

logs:
	@./scripts/deploy.sh --logs

logs-rag:
	@./scripts/deploy.sh --logs rag-service

logs-api:
	@./scripts/deploy.sh --logs api-gateway

logs-ollama:
	@./scripts/deploy.sh --logs ollama

logs-chroma:
	@./scripts/deploy.sh --logs chromadb

logs-mcp:
	@./scripts/deploy.sh --logs mcp-server

# =============================================================================
# Maintenance Commands
# =============================================================================

backup:
	@echo "Creating backup..."
	@./backup/backup.sh

backup-clean:
	@echo "Cleaning old backups..."
	@./backup/backup.sh --cleanup

backup-list:
	@echo "Listing backups..."
	@./backup/backup.sh --list

clean:
	@echo "Cleaning Docker resources..."
	@docker system prune -f

prune:
	@echo "WARNING: This will remove all unused Docker data!"
	@read -p "Are you sure? [y/N] " -n 1 -r; \
	echo; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		docker system prune -a -f --volumes; \
	fi

# =============================================================================
# Testing Commands
# =============================================================================

test:
	@echo "Running all tests..."
	@python -m pytest tests/ -v

test-unit:
	@echo "Running unit tests..."
	@python -m pytest tests/test_unit.py -v

test-int:
	@echo "Running integration tests..."
	@python -m pytest tests/test_integration.py -v

test-load:
	@echo "Starting load test server..."
	@echo "Open http://localhost:8089 to start the test"
	@locust -f tests/load_test.py --host=http://localhost:8080

benchmark:
	@echo "Running performance benchmarks..."
	@python scripts/benchmark.py --suite all --output benchmark_results.json

benchmark-llm:
	@echo "Benchmarking LLM inference..."
	@python scripts/benchmark.py --suite llm --iterations 100

benchmark-rag:
	@echo "Benchmarking RAG queries..."
	@python scripts/benchmark.py --suite rag --iterations 100

# =============================================================================
# Security Commands
# =============================================================================

security:
	@echo "Applying security hardening..."
	@./security/security-hardening.sh --apply

security-check:
	@echo "Running security checks..."
	@./security/security-hardening.sh --check

security-certs:
	@echo "Generating SSL certificates..."
	@./security/security-hardening.sh --generate-certs

# =============================================================================
# Development Commands
# =============================================================================

dev-build:
	@echo "Building development images..."
	@cd docker && docker-compose build

dev-up:
	@echo "Starting development environment..."
	@cd docker && docker-compose up

dev-down:
	@echo "Stopping development environment..."
	@cd docker && docker-compose down

dev-logs:
	@echo "Following development logs..."
	@cd docker && docker-compose logs -f

# =============================================================================
# Utility Commands
# =============================================================================

endpoints:
	@./scripts/deploy.sh --endpoints

shell-rag:
	@docker exec -it llm-rag-service /bin/sh

shell-api:
	@docker exec -it llm-api-gateway /bin/sh

shell-ollama:
	@docker exec -it llm-ollama /bin/sh

shell-chroma:
	@docker exec -it llm-chromadb /bin/sh

# =============================================================================
# Info Commands
# =============================================================================

version:
	@./scripts/deploy.sh --version

info:
	@echo "Light Local LLM System"
	@echo "======================"
	@echo ""
	@echo "Project Directory: $(PWD)"
	@echo "Docker Directory: $(PWD)/docker"
	@echo "Scripts Directory: $(PWD)/scripts"
	@echo ""
	@make version
