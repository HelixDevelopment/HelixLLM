#!/bin/bash
# HelixLLM + HelixAgent Docker Entrypoint
# =======================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if model file exists
check_model() {
    local model_path="${HELIX_MODEL_PATH:-/app/models/helix-1.5b-q4_k_m.gguf}"

    if [ ! -f "$model_path" ]; then
        log_error "Model file not found: $model_path"
        log_info "Please mount your model file to /app/models/"
        exit 1
    fi

    log_info "Model file found: $model_path"

    # Check file size
    local size=$(du -h "$model_path" | cut -f1)
    log_info "Model size: $size"
}

# Check GPU availability
check_gpu() {
    if command -v nvidia-smi &> /dev/null; then
        log_info "GPU detected:"
        nvidia-smi --query-gpu=name,memory.total,memory.free --format=csv,noheader
    else
        log_warn "No GPU detected. Running on CPU only."
    fi
}

# Initialize data directories
init_directories() {
    log_info "Initializing data directories..."

    mkdir -p /app/data/chroma
    mkdir -p /app/data/documents
    mkdir -p /app/data/sessions
    mkdir -p /app/logs

    log_info "Directories initialized"
}

# Wait for dependencies
wait_for_dependencies() {
    log_info "Checking dependencies..."

    # Wait for ChromaDB if configured
    if [ -n "$CHROMA_HOST" ]; then
        log_info "Waiting for ChromaDB at $CHROMA_HOST..."
        until curl -sf "http://$CHROMA_HOST:8000/api/v1/heartbeat" > /dev/null 2>&1; do
            sleep 1
        done
        log_info "ChromaDB is ready"
    fi
}

# Run database migrations
run_migrations() {
    log_info "Running database migrations..."
    # Add migration commands here if needed
    log_info "Migrations complete"
}

# Start the server
start_server() {
    log_info "Starting HelixLLM server..."

    # Set default config if not provided
    export HELIX_CONFIG_PATH="${HELIX_CONFIG_PATH:-/app/config.yaml}"

    # Log configuration
    log_info "Configuration: $HELIX_CONFIG_PATH"
    log_info "API Host: ${HELIX_API_HOST:-0.0.0.0}"
    log_info "API Port: ${HELIX_API_PORT:-8000}"

    # Start the server
    exec python -m helixllm.server \
        --host "${HELIX_API_HOST:-0.0.0.0}" \
        --port "${HELIX_API_PORT:-8000}" \
        --config "$HELIX_CONFIG_PATH"
}

# Start the worker
start_worker() {
    log_info "Starting HelixLLM worker..."
    exec python -m helixllm.worker
}

# Run CLI
run_cli() {
    log_info "Starting HelixLLM CLI..."
    exec python -m helixllm.cli "$@"
}

# Run tests
run_tests() {
    log_info "Running tests..."
    exec pytest /app/tests "$@"
}

# Main entrypoint
main() {
    log_info "HelixLLM + HelixAgent Container"
    log_info "================================"

    case "${1:-server}" in
        server)
            check_model
            check_gpu
            init_directories
            wait_for_dependencies
            run_migrations
            start_server
            ;;
        worker)
            check_gpu
            start_worker
            ;;
        cli)
            shift
            run_cli "$@"
            ;;
        test|tests)
            shift
            run_tests "$@"
            ;;
        bash|sh)
            exec "$@"
            ;;
        *)
            log_error "Unknown command: $1"
            log_info "Usage: $0 {server|worker|cli|test|bash}"
            exit 1
            ;;
    esac
}

# Run main function
main "$@"
