#!/bin/bash
# HelixLLM API Quickstart Script
# ===============================
# One-command setup and deployment

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_header() {
    echo -e "\n${BLUE}========================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}========================================${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

# Check prerequisites
check_prerequisites() {
    print_header "Checking Prerequisites"
    
    # Check Python
    if command -v python3 &> /dev/null; then
        PYTHON_VERSION=$(python3 --version | cut -d' ' -f2)
        print_success "Python $PYTHON_VERSION found"
    else
        print_error "Python 3 not found. Please install Python 3.8 or higher."
        exit 1
    fi
    
    # Check pip
    if command -v pip3 &> /dev/null; then
        print_success "pip3 found"
    else
        print_error "pip3 not found. Please install pip."
        exit 1
    fi
    
    # Check Docker (optional)
    if command -v docker &> /dev/null; then
        DOCKER_VERSION=$(docker --version | cut -d' ' -f3 | tr -d ',')
        print_success "Docker $DOCKER_VERSION found"
        HAS_DOCKER=true
    else
        print_info "Docker not found. Docker deployment will not be available."
        HAS_DOCKER=false
    fi
    
    # Check Docker Compose (optional)
    if command -v docker-compose &> /dev/null; then
        print_success "Docker Compose found"
        HAS_DOCKER_COMPOSE=true
    else
        print_info "Docker Compose not found."
        HAS_DOCKER_COMPOSE=false
    fi
}

# Setup Python environment
setup_python() {
    print_header "Setting Up Python Environment"
    
    # Create virtual environment if it doesn't exist
    if [ ! -d "venv" ]; then
        print_info "Creating virtual environment..."
        python3 -m venv venv
    fi
    
    # Activate virtual environment
    print_info "Activating virtual environment..."
    source venv/bin/activate
    
    # Upgrade pip
    print_info "Upgrading pip..."
    pip install --upgrade pip
    
    # Install requirements
    print_info "Installing dependencies..."
    pip install -r requirements.txt
    
    print_success "Python environment set up successfully"
}

# Create environment file
setup_env() {
    print_header "Setting Up Environment"
    
    if [ ! -f ".env" ]; then
        print_info "Creating .env file from example..."
        cp .env.example .env
        print_success ".env file created"
        print_info "Please edit .env to configure your settings"
    else
        print_info ".env file already exists"
    fi
}

# Start the server
start_server() {
    print_header "Starting HelixLLM API Server"
    
    source venv/bin/activate
    
    print_info "Starting server on http://localhost:8000"
    print_info "Press Ctrl+C to stop"
    
    python main.py
}

# Start with Docker
start_docker() {
    print_header "Starting with Docker Compose"
    
    if [ "$HAS_DOCKER" = false ] || [ "$HAS_DOCKER_COMPOSE" = false ]; then
        print_error "Docker or Docker Compose not available"
        exit 1
    fi
    
    print_info "Building and starting containers..."
    docker-compose up --build -d
    
    print_success "Containers started successfully"
    print_info "API available at: http://localhost:8000"
    print_info "View logs: docker-compose logs -f"
    print_info "Stop: docker-compose down"
}

# Run tests
run_tests() {
    print_header "Running Tests"
    
    # Check if server is running
    if ! curl -s http://localhost:8000/health > /dev/null 2>&1; then
        print_error "Server is not running. Please start the server first."
        print_info "Run: ./quickstart.sh start"
        exit 1
    fi
    
    print_info "Running test suite..."
    
    if [ -f "test_api.py" ]; then
        source venv/bin/activate
        python test_api.py
    else
        print_error "Test script not found"
        exit 1
    fi
}

# Show usage
show_usage() {
    echo "HelixLLM API Quickstart"
    echo ""
    echo "Usage: $0 [command]"
    echo ""
    echo "Commands:"
    echo "  setup       - Set up the environment (install dependencies)"
    echo "  start       - Start the API server locally"
    echo "  docker      - Start with Docker Compose"
    echo "  test        - Run the test suite"
    echo "  stop        - Stop Docker containers"
    echo "  help        - Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 setup    # One-time setup"
    echo "  $0 start    # Start the server"
    echo "  $0 test     # Run tests"
}

# Main
case "${1:-}" in
    setup)
        check_prerequisites
        setup_python
        setup_env
        print_header "Setup Complete"
        print_success "HelixLLM API is ready to use!"
        print_info "Next steps:"
        print_info "  1. Edit .env to configure your settings"
        print_info "  2. Run: $0 start"
        ;;
    start)
        check_prerequisites
        if [ ! -d "venv" ]; then
            setup_python
        fi
        start_server
        ;;
    docker)
        start_docker
        ;;
    test)
        run_tests
        ;;
    stop)
        print_header "Stopping Docker Containers"
        docker-compose down
        print_success "Containers stopped"
        ;;
    help|--help|-h)
        show_usage
        ;;
    *)
        echo "HelixLLM API Quickstart"
        echo ""
        echo "No command specified. Running full setup..."
        echo ""
        check_prerequisites
        setup_python
        setup_env
        print_header "Setup Complete"
        print_success "HelixLLM API is ready to use!"
        echo ""
        print_info "To start the server:"
        print_info "  ./quickstart.sh start"
        echo ""
        print_info "To start with Docker:"
        print_info "  ./quickstart.sh docker"
        echo ""
        print_info "For more options:"
        print_info "  ./quickstart.sh help"
        ;;
esac
