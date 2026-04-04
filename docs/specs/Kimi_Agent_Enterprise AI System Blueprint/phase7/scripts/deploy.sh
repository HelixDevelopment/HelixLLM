#!/bin/bash
# =============================================================================
# Light Local LLM System - Deployment Script
# =============================================================================
# This script handles deployment operations:
# - Initial deployment
# - Service updates
# - Rollback procedures
# - Health verification
# - Blue-green deployment support
#
# Usage:
#   ./deploy.sh --init              # Initial deployment
#   ./deploy.sh --update            # Update services
#   ./deploy.sh --rollback          # Rollback to previous version
#   ./deploy.sh --status            # Check deployment status
# =============================================================================

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
DOCKER_DIR="${PROJECT_DIR}/docker"
BACKUP_DIR="${PROJECT_DIR}/backups"
DEPLOY_LOG="${PROJECT_DIR}/deploy.log"
VERSION_FILE="${PROJECT_DIR}/.version"

# Docker Compose files
COMPOSE_FILE="${DOCKER_DIR}/docker-compose.yml"
COMPOSE_OVERRIDE="${DOCKER_DIR}/docker-compose.override.yml"

# Deployment settings
HEALTH_CHECK_TIMEOUT=300
HEALTH_CHECK_INTERVAL=5
ROLLBACK_ON_FAILURE=true

# =============================================================================
# Colors
# =============================================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# =============================================================================
# Logging
# =============================================================================

log() {
    echo -e "${BLUE}[$(date '+%Y-%m-%d %H:%M:%S')]${NC} $1" | tee -a "$DEPLOY_LOG"
}

success() {
    echo -e "${GREEN}[$(date '+%Y-%m-%d %H:%M:%S')] ✓${NC} $1" | tee -a "$DEPLOY_LOG"
}

warning() {
    echo -e "${YELLOW}[$(date '+%Y-%m-%d %H:%M:%S')] ⚠${NC} $1" | tee -a "$DEPLOY_LOG"
}

error() {
    echo -e "${RED}[$(date '+%Y-%m-%d %H:%M:%S')] ✗${NC} $1" | tee -a "$DEPLOY_LOG"
}

info() {
    echo -e "${CYAN}[$(date '+%Y-%m-%d %H:%M:%S')] ℹ${NC} $1" | tee -a "$DEPLOY_LOG"
}

# =============================================================================
# Utility Functions
# =============================================================================

check_dependencies() {
    local deps=("docker" "docker-compose")
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            error "Required dependency '$dep' not found"
            exit 1
        fi
    done
    
    # Check Docker is running
    if ! docker info &> /dev/null; then
        error "Docker daemon is not running"
        exit 1
    fi
    
    success "All dependencies satisfied"
}

get_version() {
    if [[ -f "$VERSION_FILE" ]]; then
        cat "$VERSION_FILE"
    else
        echo "unknown"
    fi
}

set_version() {
    echo "$1" > "$VERSION_FILE"
}

# =============================================================================
# Health Check Functions
# =============================================================================

check_service_health() {
    local service=$1
    local url=$2
    local max_attempts=$((HEALTH_CHECK_TIMEOUT / HEALTH_CHECK_INTERVAL))
    
    log "Checking health of $service..."
    
    for ((i=1; i<=max_attempts; i++)); do
        if curl -sf "$url" > /dev/null 2>&1; then
            success "$service is healthy"
            return 0
        fi
        
        info "Attempt $i/$max_attempts: $service not ready yet, waiting..."
        sleep $HEALTH_CHECK_INTERVAL
    done
    
    error "$service failed health check after ${HEALTH_CHECK_TIMEOUT}s"
    return 1
}

check_all_services() {
    log "Checking all services health..."
    
    local services=(
        "ollama:http://localhost:11434/api/tags"
        "chromadb:http://localhost:8000/api/v1/heartbeat"
        "rag-service:http://localhost:8001/health"
        "mcp-server:http://localhost:3000/health"
        "api-gateway:http://localhost:8080/health"
        "prometheus:http://localhost:9090/-/healthy"
        "grafana:http://localhost:3001/api/health"
    )
    
    local failed=0
    for service in "${services[@]}"; do
        IFS=':' read -r name url <<< "$service"
        if ! check_service_health "$name" "$url"; then
            ((failed++))
        fi
    done
    
    return $failed
}

# =============================================================================
# Deployment Functions
# =============================================================================

initial_deployment() {
    log "Starting initial deployment..."
    
    # Create necessary directories
    mkdir -p "${PROJECT_DIR}/"{backups,logs,data}
    
    # Check for environment file
    if [[ ! -f "${DOCKER_DIR}/.env" ]]; then
        if [[ -f "${DOCKER_DIR}/.env.example" ]]; then
            warning "Environment file not found, copying from example..."
            cp "${DOCKER_DIR}/.env.example" "${DOCKER_DIR}/.env"
            warning "Please edit ${DOCKER_DIR}/.env with your configuration"
        else
            error "No environment file found"
            exit 1
        fi
    fi
    
    # Pull latest images
    log "Pulling Docker images..."
    docker-compose -f "$COMPOSE_FILE" pull
    
    # Start infrastructure services first
    log "Starting infrastructure services..."
    docker-compose -f "$COMPOSE_FILE" up -d chromadb redis
    sleep 10
    
    # Start core services
    log "Starting core services..."
    docker-compose -f "$COMPOSE_FILE" up -d ollama
    sleep 5
    
    # Pull default model
    log "Pulling default LLM model..."
    docker exec llm-ollama ollama pull llama3.2 || warning "Model pull may have failed"
    
    # Start application services
    log "Starting application services..."
    docker-compose -f "$COMPOSE_FILE" up -d rag-service mcp-server api-gateway
    
    # Start monitoring services
    log "Starting monitoring services..."
    docker-compose -f "$COMPOSE_FILE" up -d prometheus grafana loki promtail
    
    # Start reverse proxy
    log "Starting reverse proxy..."
    docker-compose -f "$COMPOSE_FILE" up -d traefik
    
    # Wait for services to be ready
    log "Waiting for services to be ready..."
    sleep 30
    
    # Health check
    if check_all_services; then
        success "Initial deployment completed successfully!"
        set_version "$(date +%Y%m%d-%H%M%S)"
        
        info ""
        info "Services are available at:"
        info "  API Gateway:    http://localhost:8080"
        info "  Grafana:        http://localhost:3001"
        info "  Prometheus:     http://localhost:9090"
        info "  Traefik Dashboard: http://localhost:8080"
    else
        error "Some services failed to start properly"
        return 1
    fi
}

update_services() {
    log "Starting service update..."
    
    # Create backup before update
    log "Creating pre-update backup..."
    "${PROJECT_DIR}/backup/backup.sh" --config-only
    
    # Record current version for potential rollback
    local previous_version
    previous_version=$(get_version)
    echo "$previous_version" > "${PROJECT_DIR}/.version.backup"
    
    # Pull latest images
    log "Pulling latest Docker images..."
    docker-compose -f "$COMPOSE_FILE" pull
    
    # Rolling update - update services one by one
    local services=("mcp-server" "rag-service" "api-gateway")
    
    for service in "${services[@]}"; do
        log "Updating $service..."
        
        # Stop and remove old container
        docker-compose -f "$COMPOSE_FILE" stop "$service"
        docker-compose -f "$COMPOSE_FILE" rm -f "$service"
        
        # Start new container
        docker-compose -f "$COMPOSE_FILE" up -d "$service"
        
        # Wait for service to be healthy
        sleep 10
        
        # Check health
        local health_url
        case "$service" in
            "mcp-server") health_url="http://localhost:3000/health" ;;
            "rag-service") health_url="http://localhost:8001/health" ;;
            "api-gateway") health_url="http://localhost:8080/health" ;;
        esac
        
        if ! check_service_health "$service" "$health_url"; then
            error "$service update failed!"
            
            if [[ "$ROLLBACK_ON_FAILURE" == "true" ]]; then
                warning "Initiating rollback..."
                rollback_deployment
            fi
            return 1
        fi
        
        success "$service updated successfully"
    done
    
    # Update version
    set_version "$(date +%Y%m%d-%H%M%S)"
    
    success "Service update completed successfully!"
}

rollback_deployment() {
    log "Starting rollback..."
    
    # Check for backup version
    if [[ ! -f "${PROJECT_DIR}/.version.backup" ]]; then
        error "No backup version found for rollback"
        return 1
    fi
    
    local backup_version
    backup_version=$(cat "${PROJECT_DIR}/.version.backup")
    
    log "Rolling back to version: $backup_version"
    
    # Restore from backup if available
    local latest_backup
    latest_backup=$(ls -t "${BACKUP_DIR}"/config/*.tar.gz 2>/dev/null | head -1)
    
    if [[ -n "$latest_backup" ]]; then
        log "Restoring configuration from backup..."
        tar xzf "$latest_backup" -C "$PROJECT_DIR"
    fi
    
    # Restart services with previous configuration
    docker-compose -f "$COMPOSE_FILE" down
    docker-compose -f "$COMPOSE_FILE" up -d
    
    # Restore version
    mv "${PROJECT_DIR}/.version.backup" "$VERSION_FILE"
    
    success "Rollback completed!"
}

# =============================================================================
# Service Management
# =============================================================================

start_services() {
    log "Starting all services..."
    docker-compose -f "$COMPOSE_FILE" up -d
    success "Services started"
}

stop_services() {
    log "Stopping all services..."
    docker-compose -f "$COMPOSE_FILE" down
    success "Services stopped"
}

restart_services() {
    log "Restarting all services..."
    docker-compose -f "$COMPOSE_FILE" restart
    success "Services restarted"
}

restart_service() {
    local service=$1
    log "Restarting $service..."
    docker-compose -f "$COMPOSE_FILE" restart "$service"
    success "$service restarted"
}

view_logs() {
    local service=${1:-}
    
    if [[ -n "$service" ]]; then
        docker-compose -f "$COMPOSE_FILE" logs -f "$service"
    else
        docker-compose -f "$COMPOSE_FILE" logs -f
    fi
}

# =============================================================================
# Status and Info
# =============================================================================

show_status() {
    echo ""
    echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}              Light Local LLM System - Status${NC}"
    echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
    
    # Show version
    echo -e "${BLUE}Version:${NC} $(get_version)"
    echo ""
    
    # Show running containers
    echo -e "${BLUE}Running Containers:${NC}"
    docker-compose -f "$COMPOSE_FILE" ps
    echo ""
    
    # Show resource usage
    echo -e "${BLUE}Resource Usage:${NC}"
    docker stats --no-stream --format "table {{.Container}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}\t{{.PIDs}}"
    echo ""
    
    # Show service health
    echo -e "${BLUE}Service Health:${NC}"
    
    local services=(
        "Ollama:http://localhost:11434/api/tags"
        "ChromaDB:http://localhost:8000/api/v1/heartbeat"
        "RAG Service:http://localhost:8001/health"
        "MCP Server:http://localhost:3000/health"
        "API Gateway:http://localhost:8080/health"
        "Prometheus:http://localhost:9090/-/healthy"
        "Grafana:http://localhost:3001/api/health"
    )
    
    for service in "${services[@]}"; do
        IFS=':' read -r name url <<< "$service"
        if curl -sf "$url" > /dev/null 2>&1; then
            echo -e "  $name: ${GREEN}✓ Healthy${NC}"
        else
            echo -e "  $name: ${RED}✗ Unhealthy${NC}"
        fi
    done
    
    echo ""
}

show_endpoints() {
    echo ""
    echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}                    Service Endpoints${NC}"
    echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
    echo -e "${BLUE}API Gateway:${NC}      http://localhost:8080"
    echo -e "${BLUE}Ollama:${NC}           http://localhost:11434"
    echo -e "${BLUE}ChromaDB:${NC}         http://localhost:8000"
    echo -e "${BLUE}RAG Service:${NC}      http://localhost:8001"
    echo -e "${BLUE}MCP Server:${NC}       http://localhost:3000"
    echo ""
    echo -e "${BLUE}Grafana:${NC}          http://localhost:3001"
    echo -e "${BLUE}Prometheus:${NC}       http://localhost:9090"
    echo -e "${BLUE}Alertmanager:${NC}     http://localhost:9093"
    echo -e "${BLUE}Loki:${NC}             http://localhost:3100"
    echo -e "${BLUE}Traefik Dashboard:${NC} http://localhost:8080"
    echo ""
}

# =============================================================================
# Cleanup Functions
# =============================================================================

cleanup() {
    log "Cleaning up..."
    
    # Remove stopped containers
    docker container prune -f
    
    # Remove unused images
    docker image prune -f
    
    # Remove unused volumes
    docker volume prune -f
    
    # Remove unused networks
    docker network prune -f
    
    success "Cleanup completed"
}

# =============================================================================
# Main Functions
# =============================================================================

show_help() {
    cat << EOF
Light Local LLM System - Deployment Script

Usage:
  $(basename "$0") [COMMAND] [OPTIONS]

Commands:
  --init, -i              Initial deployment
  --update, -u            Update services
  --rollback, -r          Rollback to previous version
  --start                 Start all services
  --stop                  Stop all services
  --restart               Restart all services
  --restart-service <s>   Restart specific service
  --status, -s            Show deployment status
  --endpoints, -e         Show service endpoints
  --logs [service]        View logs (optionally for specific service)
  --health                Run health checks
  --cleanup               Clean up Docker resources
  --version               Show current version
  --help, -h              Show this help message

Examples:
  $(basename "$0") --init                    # First time deployment
  $(basename "$0") --update                  # Update all services
  $(basename "$0") --restart-service ollama  # Restart Ollama only
  $(basename "$0") --logs api-gateway        # View API Gateway logs
EOF
}

# =============================================================================
# Main Entry Point
# =============================================================================

main() {
    # Ensure log file exists
    touch "$DEPLOY_LOG"
    
    case "${1:-}" in
        --help|-h)
            show_help
            exit 0
            ;;
        --init|-i)
            check_dependencies
            initial_deployment
            ;;
        --update|-u)
            check_dependencies
            update_services
            ;;
        --rollback|-r)
            check_dependencies
            rollback_deployment
            ;;
        --start)
            check_dependencies
            start_services
            ;;
        --stop)
            stop_services
            ;;
        --restart)
            check_dependencies
            restart_services
            ;;
        --restart-service)
            if [[ -z "${2:-}" ]]; then
                error "Please specify a service name"
                exit 1
            fi
            check_dependencies
            restart_service "$2"
            ;;
        --status|-s)
            show_status
            ;;
        --endpoints|-e)
            show_endpoints
            ;;
        --logs)
            view_logs "${2:-}"
            ;;
        --health)
            check_all_services
            ;;
        --cleanup)
            cleanup
            ;;
        --version)
            echo "Version: $(get_version)"
            ;;
        *)
            error "Unknown command: ${1:-}"
            show_help
            exit 1
            ;;
    esac
}

main "$@"
