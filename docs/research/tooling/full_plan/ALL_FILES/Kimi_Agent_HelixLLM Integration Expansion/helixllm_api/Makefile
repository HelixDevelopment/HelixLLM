# HelixLLM API Makefile
# =====================
# Convenient commands for development and deployment

.PHONY: help install start stop test docker-build docker-up docker-down clean lint format

# Default target
help:
	@echo "HelixLLM API - Available Commands"
	@echo "=================================="
	@echo ""
	@echo "Development:"
	@echo "  make install      - Install Python dependencies"
	@echo "  make start        - Start the API server locally"
	@echo "  make dev          - Start with auto-reload"
	@echo "  make stop         - Stop the running server"
	@echo ""
	@echo "Testing:"
	@echo "  make test         - Run all tests"
	@echo "  make test-bash    - Run bash test suite"
	@echo "  make test-python  - Run Python test suite"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-build - Build Docker image"
	@echo "  make docker-up    - Start with Docker Compose"
	@echo "  make docker-down  - Stop Docker containers"
	@echo "  make docker-logs  - View Docker logs"
	@echo ""
	@echo "Code Quality:"
	@echo "  make lint         - Run linter"
	@echo "  make format       - Format code"
	@echo ""
	@echo "Maintenance:"
	@echo "  make clean        - Clean up generated files"
	@echo "  make setup        - Full setup (install + configure)"

# Development
install:
	pip install -r requirements.txt

start:
	python main.py

dev:
	uvicorn main:app --host 0.0.0.0 --port 8000 --reload

stop:
	-pkill -f "uvicorn main:app" || true
	-pkill -f "python main.py" || true

# Testing
test: test-python

test-bash:
	chmod +x test_api.sh
	./test_api.sh

test-python:
	python test_api.py

# Docker
docker-build:
	docker build -t helixllm-api .

docker-up:
	docker-compose up --build -d

docker-down:
	docker-compose down

docker-logs:
	docker-compose logs -f helixllm-api

docker-nginx:
	docker-compose --profile with-nginx up --build -d

docker-monitor:
	docker-compose --profile monitoring up --build -d

# Code Quality
lint:
	@echo "Running linter..."
	@which flake8 > /dev/null 2>&1 && flake8 main.py || echo "flake8 not installed, skipping"
	@which pylint > /dev/null 2>&1 && pylint main.py || echo "pylint not installed, skipping"
	@which mypy > /dev/null 2>&1 && mypy main.py || echo "mypy not installed, skipping"

format:
	@echo "Formatting code..."
	@which black > /dev/null 2>&1 && black main.py || echo "black not installed, skipping"
	@which isort > /dev/null 2>&1 && isort main.py || echo "isort not installed, skipping"

# Maintenance
clean:
	@echo "Cleaning up..."
	@find . -type d -name "__pycache__" -exec rm -rf {} + 2>/dev/null || true
	@find . -type f -name "*.pyc" -delete 2>/dev/null || true
	@find . -type f -name "*.pyo" -delete 2>/dev/null || true
	@find . -type f -name "*.pyd" -delete 2>/dev/null || true
	@find . -type d -name "*.egg-info" -exec rm -rf {} + 2>/dev/null || true
	@find . -type d -name ".pytest_cache" -exec rm -rf {} + 2>/dev/null || true
	@find . -type d -name ".mypy_cache" -exec rm -rf {} + 2>/dev/null || true
	@rm -rf build/ dist/ 2>/dev/null || true
	@echo "Cleanup complete"

setup:
	@echo "Setting up HelixLLM API..."
	@chmod +x quickstart.sh
	@./quickstart.sh setup

# API Testing Helpers
api-health:
	@curl -s http://localhost:8000/health | jq . || curl -s http://localhost:8000/health

api-models:
	@curl -s http://localhost:8000/v1/models | jq . || curl -s http://localhost:8000/v1/models

api-chat:
	@curl -s -X POST http://localhost:8000/v1/chat/completions \
		-H "Content-Type: application/json" \
		-d '{"model":"helix-llm","messages":[{"role":"user","content":"Hello!"}]}' | jq . || \
	curl -s -X POST http://localhost:8000/v1/chat/completions \
		-H "Content-Type: application/json" \
		-d '{"model":"helix-llm","messages":[{"role":"user","content":"Hello!"}]}'

api-stream:
	@curl -s -X POST http://localhost:8000/v1/chat/completions \
		-H "Content-Type: application/json" \
		-d '{"model":"helix-llm","messages":[{"role":"user","content":"Hello!"}],"stream":true}'

api-embed:
	@curl -s -X POST http://localhost:8000/v1/embeddings \
		-H "Content-Type: application/json" \
		-d '{"model":"helix-llm","input":"Hello world"}' | jq . || \
	curl -s -X POST http://localhost:8000/v1/embeddings \
		-H "Content-Type: application/json" \
		-d '{"model":"helix-llm","input":"Hello world"}'
