#!/bin/bash
# =============================================================================
# Light Local LLM System - Backup Script
# =============================================================================
# This script performs comprehensive backups of:
# - ChromaDB vector database
# - Configuration files
# - Logs
# - Custom models (if applicable)
#
# Usage:
#   ./backup.sh                    # Full backup
#   ./backup.sh --incremental      # Incremental backup
#   ./backup.sh --config-only      # Configuration only
#   ./backup.sh --restore <file>   # Restore from backup
#
# Environment Variables:
#   BACKUP_DIR          - Backup destination directory
#   BACKUP_RETENTION    - Number of days to keep backups
#   S3_BUCKET           - S3 bucket for remote backup (optional)
# =============================================================================

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BACKUP_DIR="${BACKUP_DIR:-$PROJECT_DIR/backups}"
BACKUP_RETENTION="${BACKUP_RETENTION:-30}"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_NAME="llm_backup_${TIMESTAMP}"
LOG_FILE="${BACKUP_DIR}/backup_${TIMESTAMP}.log"

# Docker compose file location
DOCKER_COMPOSE_FILE="${PROJECT_DIR}/docker/docker-compose.yml"

# Service data directories (in containers)
CHROMA_DATA_DIR="/chroma/chroma"
OLLAMA_DATA_DIR="/root/.ollama"
GRAFANA_DATA_DIR="/var/lib/grafana"
PROMETHEUS_DATA_DIR="/prometheus"

# =============================================================================
# Colors for output
# =============================================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# =============================================================================
# Logging Functions
# =============================================================================

log() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1" | tee -a "$LOG_FILE"
}

log_success() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')] ✓${NC} $1" | tee -a "$LOG_FILE"
}

log_warning() {
    echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] ⚠${NC} $1" | tee -a "$LOG_FILE"
}

log_error() {
    echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] ✗${NC} $1" | tee -a "$LOG_FILE"
}

# =============================================================================
# Utility Functions
# =============================================================================

check_dependencies() {
    local deps=("docker" "docker-compose")
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            log_error "Required dependency '$dep' not found"
            exit 1
        fi
    done
}

ensure_backup_dir() {
    if [[ ! -d "$BACKUP_DIR" ]]; then
        log "Creating backup directory: $BACKUP_DIR"
        mkdir -p "$BACKUP_DIR"
    fi
    
    # Create subdirectories
    mkdir -p "${BACKUP_DIR}/{full,incremental,config,logs}"
}

get_container_id() {
    local service_name=$1
    docker-compose -f "$DOCKER_COMPOSE_FILE" ps -q "$service_name" 2>/dev/null
}

# =============================================================================
# Backup Functions
# =============================================================================

backup_chromadb() {
    log "Backing up ChromaDB..."
    
    local container_id
    container_id=$(get_container_id "chromadb")
    
    if [[ -z "$container_id" ]]; then
        log_warning "ChromaDB container not running, attempting to start..."
        docker-compose -f "$DOCKER_COMPOSE_FILE" up -d chromadb
        sleep 5
        container_id=$(get_container_id "chromadb")
    fi
    
    if [[ -n "$container_id" ]]; then
        local backup_file="${BACKUP_DIR}/full/${BACKUP_NAME}_chromadb.tar.gz"
        
        # Create backup using docker exec
        docker exec "$container_id" tar czf - "$CHROMA_DATA_DIR" > "$backup_file"
        
        if [[ -f "$backup_file" ]]; then
            log_success "ChromaDB backed up to: $backup_file"
            echo "chromadb:$backup_file" >> "${BACKUP_DIR}/full/${BACKUP_NAME}_manifest.txt"
        else
            log_error "Failed to backup ChromaDB"
        fi
    else
        log_error "ChromaDB container not available"
    fi
}

backup_ollama() {
    log "Backing up Ollama models..."
    
    local container_id
    container_id=$(get_container_id "ollama")
    
    if [[ -n "$container_id" ]]; then
        local backup_file="${BACKUP_DIR}/full/${BACKUP_NAME}_ollama.tar.gz"
        
        docker exec "$container_id" tar czf - "$OLLAMA_DATA_DIR" > "$backup_file"
        
        if [[ -f "$backup_file" ]]; then
            log_success "Ollama backed up to: $backup_file"
            echo "ollama:$backup_file" >> "${BACKUP_DIR}/full/${BACKUP_NAME}_manifest.txt"
        else
            log_error "Failed to backup Ollama"
        fi
    else
        log_warning "Ollama container not running, skipping..."
    fi
}

backup_configurations() {
    log "Backing up configuration files..."
    
    local config_backup="${BACKUP_DIR}/config/${BACKUP_NAME}_config.tar.gz"
    
    # Backup all configuration files
    tar czf "$config_backup" \
        -C "$PROJECT_DIR" \
        docker/ \
        monitoring/ \
        2>/dev/null || true
    
    # Backup environment files (if they exist)
    if [[ -f "${PROJECT_DIR}/docker/.env" ]]; then
        cp "${PROJECT_DIR}/docker/.env" "${BACKUP_DIR}/config/${BACKUP_NAME}_env_backup"
    fi
    
    if [[ -f "$config_backup" ]]; then
        log_success "Configurations backed up to: $config_backup"
        echo "config:$config_backup" >> "${BACKUP_DIR}/full/${BACKUP_NAME}_manifest.txt"
    else
        log_warning "No configuration files found to backup"
    fi
}

backup_logs() {
    log "Backing up recent logs..."
    
    local log_backup="${BACKUP_DIR}/logs/${BACKUP_NAME}_logs.tar.gz"
    
    # Collect logs from all services
    mkdir -p "${BACKUP_DIR}/logs/temp"
    
    for service in ollama chromadb rag-service mcp-server api-gateway; do
        docker-compose -f "$DOCKER_COMPOSE_FILE" logs --tail=10000 "$service" \
            > "${BACKUP_DIR}/logs/temp/${service}.log" 2>&1 || true
    done
    
    # Compress logs
    tar czf "$log_backup" -C "${BACKUP_DIR}/logs/temp" . 2>/dev/null || true
    rm -rf "${BACKUP_DIR}/logs/temp"
    
    if [[ -f "$log_backup" ]]; then
        log_success "Logs backed up to: $log_backup"
        echo "logs:$log_backup" >> "${BACKUP_DIR}/full/${BACKUP_NAME}_manifest.txt"
    fi
}

backup_grafana() {
    log "Backing up Grafana dashboards and data..."
    
    local container_id
    container_id=$(get_container_id "grafana")
    
    if [[ -n "$container_id" ]]; then
        local backup_file="${BACKUP_DIR}/full/${BACKUP_NAME}_grafana.tar.gz"
        
        docker exec "$container_id" tar czf - "$GRAFANA_DATA_DIR" > "$backup_file" 2>/dev/null || true
        
        if [[ -f "$backup_file" ]]; then
            log_success "Grafana backed up to: $backup_file"
            echo "grafana:$backup_file" >> "${BACKUP_DIR}/full/${BACKUP_NAME}_manifest.txt"
        fi
    fi
}

backup_prometheus() {
    log "Backing up Prometheus data..."
    
    local container_id
    container_id=$(get_container_id "prometheus")
    
    if [[ -n "$container_id" ]]; then
        # Create snapshot
        local snapshot_response
        snapshot_response=$(docker exec "$container_id" \
            wget -qO- --post-data='' http://localhost:9090/api/v1/admin/tsdb/snapshot 2>/dev/null || echo '{}')
        
        local snapshot_name
        snapshot_name=$(echo "$snapshot_response" | grep -oP '"name":"[^"]+"' | cut -d'"' -f4)
        
        if [[ -n "$snapshot_name" ]]; then
            local backup_file="${BACKUP_DIR}/full/${BACKUP_NAME}_prometheus.tar.gz"
            
            docker exec "$container_id" tar czf - "${PROMETHEUS_DATA_DIR}/snapshots/${snapshot_name}" \
                > "$backup_file" 2>/dev/null || true
            
            if [[ -f "$backup_file" ]]; then
                log_success "Prometheus backed up to: $backup_file"
                echo "prometheus:$backup_file" >> "${BACKUP_DIR}/full/${BACKUP_NAME}_manifest.txt"
            fi
        fi
    fi
}

create_backup_manifest() {
    log "Creating backup manifest..."
    
    local manifest="${BACKUP_DIR}/full/${BACKUP_NAME}_manifest.txt"
    
    cat > "$manifest" << EOF
Backup Name: $BACKUP_NAME
Timestamp: $(date -Iseconds)
Hostname: $(hostname)
Docker Version: $(docker --version)
Backup Type: ${BACKUP_TYPE:-full}

Components:
EOF

    # Add component sizes
    while IFS=: read -r component file; do
        if [[ -f "$file" ]]; then
            local size
            size=$(du -h "$file" | cut -f1)
            echo "  $component: $size" >> "$manifest"
        fi
    done < "$manifest"
    
    log_success "Backup manifest created: $manifest"
}

upload_to_s3() {
    if [[ -n "${S3_BUCKET:-}" ]]; then
        log "Uploading backup to S3..."
        
        local s3_path="s3://${S3_BUCKET}/llm-backups/${BACKUP_NAME}"
        
        # Upload all backup files
        for file in "${BACKUP_DIR}/full/${BACKUP_NAME}"_*; do
            if [[ -f "$file" ]]; then
                aws s3 cp "$file" "$s3_path/" 2>/dev/null || {
                    log_warning "Failed to upload to S3"
                    return 1
                }
            fi
        done
        
        log_success "Backup uploaded to S3: $s3_path"
    fi
}

# =============================================================================
# Restore Functions
# =============================================================================

restore_chromadb() {
    local backup_file=$1
    log "Restoring ChromaDB from: $backup_file"
    
    local container_id
    container_id=$(get_container_id "chromadb")
    
    if [[ -n "$container_id" ]]; then
        # Stop the service
        docker-compose -f "$DOCKER_COMPOSE_FILE" stop chromadb
        
        # Restore data
        docker run --rm \
            -v "llm_chroma-data:/data" \
            -v "$backup_file:/backup.tar.gz:ro" \
            alpine sh -c "rm -rf /data/* && tar xzf /backup.tar.gz -C /data --strip-components=2"
        
        # Restart service
        docker-compose -f "$DOCKER_COMPOSE_FILE" start chromadb
        
        log_success "ChromaDB restored successfully"
    else
        log_error "ChromaDB container not found"
    fi
}

restore_ollama() {
    local backup_file=$1
    log "Restoring Ollama from: $backup_file"
    
    docker-compose -f "$DOCKER_COMPOSE_FILE" stop ollama
    
    docker run --rm \
        -v "llm_ollama-models:/data" \
        -v "$backup_file:/backup.tar.gz:ro" \
        alpine sh -c "rm -rf /data/* && tar xzf /backup.tar.gz -C /data --strip-components=2"
    
    docker-compose -f "$DOCKER_COMPOSE_FILE" start ollama
    
    log_success "Ollama restored successfully"
}

restore_configurations() {
    local backup_file=$1
    log "Restoring configurations from: $backup_file"
    
    tar xzf "$backup_file" -C "$PROJECT_DIR"
    
    log_success "Configurations restored successfully"
}

restore_from_backup() {
    local backup_file=$1
    
    if [[ ! -f "$backup_file" ]]; then
        log_error "Backup file not found: $backup_file"
        exit 1
    fi
    
    log "Starting restore from: $backup_file"
    
    # Detect backup type from filename
    if [[ "$backup_file" == *"chromadb"* ]]; then
        restore_chromadb "$backup_file"
    elif [[ "$backup_file" == *"ollama"* ]]; then
        restore_ollama "$backup_file"
    elif [[ "$backup_file" == *"config"* ]]; then
        restore_configurations "$backup_file"
    else
        log_error "Unknown backup type"
        exit 1
    fi
}

# =============================================================================
# Cleanup Functions
# =============================================================================

cleanup_old_backups() {
    log "Cleaning up backups older than $BACKUP_RETENTION days..."
    
    find "$BACKUP_DIR" -name "llm_backup_*" -type f -mtime +$BACKUP_RETENTION -delete
    
    local deleted_count
    deleted_count=$(find "$BACKUP_DIR" -name "llm_backup_*" -type f -mtime +$BACKUP_RETENTION | wc -l)
    
    log_success "Cleanup completed. Removed $deleted_count old backups."
}

# =============================================================================
# Main Functions
# =============================================================================

show_help() {
    cat << EOF
Light Local LLM System - Backup Script

Usage:
  $(basename "$0") [OPTIONS]

Options:
  --full              Perform full backup (default)
  --incremental       Perform incremental backup
  --config-only       Backup only configuration files
  --restore <file>    Restore from backup file
  --list              List available backups
  --cleanup           Clean up old backups
  --verify            Verify backup integrity
  --help              Show this help message

Environment Variables:
  BACKUP_DIR          Backup destination directory (default: ./backups)
  BACKUP_RETENTION    Days to keep backups (default: 30)
  S3_BUCKET           S3 bucket for remote backup (optional)

Examples:
  $(basename "$0")                    # Full backup
  $(basename "$0") --incremental      # Incremental backup
  $(basename "$0") --restore ./backups/llm_backup_20240101_chromadb.tar.gz
  $(basename "$0") --cleanup          # Remove old backups
EOF
}

list_backups() {
    log "Available backups:"
    
    if [[ -d "$BACKUP_DIR/full" ]]; then
        echo ""
        echo "Full Backups:"
        ls -lh "$BACKUP_DIR/full"/*.tar.gz 2>/dev/null | awk '{print "  " $9 " (" $5 ")"}' || echo "  None"
    fi
    
    if [[ -d "$BACKUP_DIR/config" ]]; then
        echo ""
        echo "Configuration Backups:"
        ls -lh "$BACKUP_DIR/config"/*.tar.gz 2>/dev/null | awk '{print "  " $9 " (" $5 ")"}' || echo "  None"
    fi
}

verify_backup() {
    local backup_file=$1
    
    if [[ ! -f "$backup_file" ]]; then
        log_error "Backup file not found: $backup_file"
        return 1
    fi
    
    log "Verifying backup: $backup_file"
    
    # Test archive integrity
    if tar tzf "$backup_file" > /dev/null 2>&1; then
        log_success "Backup is valid"
        return 0
    else
        log_error "Backup is corrupted"
        return 1
    fi
}

perform_full_backup() {
    log "Starting full backup..."
    log "Backup directory: $BACKUP_DIR"
    log "Backup name: $BACKUP_NAME"
    
    ensure_backup_dir
    
    backup_chromadb
    backup_ollama
    backup_configurations
    backup_logs
    backup_grafana
    backup_prometheus
    
    create_backup_manifest
    upload_to_s3
    cleanup_old_backups
    
    log_success "Full backup completed successfully!"
    log "Backup location: ${BACKUP_DIR}/full/${BACKUP_NAME}_*"
}

perform_incremental_backup() {
    log "Starting incremental backup..."
    
    ensure_backup_dir
    
    # Only backup data that has changed since last backup
    backup_chromadb
    backup_logs
    
    log_success "Incremental backup completed!"
}

# =============================================================================
# Main Entry Point
# =============================================================================

main() {
    check_dependencies
    
    case "${1:-}" in
        --help|-h)
            show_help
            exit 0
            ;;
        --restore)
            if [[ -z "${2:-}" ]]; then
                log_error "Please specify a backup file to restore"
                exit 1
            fi
            restore_from_backup "$2"
            ;;
        --list)
            list_backups
            ;;
        --cleanup)
            cleanup_old_backups
            ;;
        --verify)
            if [[ -z "${2:-}" ]]; then
                log_error "Please specify a backup file to verify"
                exit 1
            fi
            verify_backup "$2"
            ;;
        --incremental)
            perform_incremental_backup
            ;;
        --config-only)
            ensure_backup_dir
            backup_configurations
            ;;
        --full|"")
            perform_full_backup
            ;;
        *)
            log_error "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
}

# Run main function
main "$@"
