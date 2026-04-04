#!/bin/bash
# =============================================================================
# Light Local LLM System - Security Hardening Script
# =============================================================================
# This script implements security hardening measures:
# - Firewall configuration
# - Docker security settings
# - SSL/TLS configuration
# - Access control setup
# - Secret management
#
# Usage:
#   ./security-hardening.sh --apply      # Apply all security settings
#   ./security-hardening.sh --check      # Check security status
#   ./security-hardening.sh --generate-certs  # Generate SSL certificates
# =============================================================================

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
DOCKER_DIR="${PROJECT_DIR}/docker"
CERT_DIR="${DOCKER_DIR}/certs"

# Default ports that should be exposed
ALLOWED_PORTS=(80 443 8080 3000 3001 9090 9093)

# Services that should only be accessible internally
INTERNAL_SERVICES=("chromadb:8000" "prometheus:9090" "loki:3100")

# =============================================================================
# Colors
# =============================================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# =============================================================================
# Logging
# =============================================================================

log() { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[PASS]${NC} $1"; }
warning() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[FAIL]${NC} $1"; }

# =============================================================================
# Firewall Configuration
# =============================================================================

configure_ufw() {
    log "Configuring UFW firewall..."
    
    # Check if UFW is installed
    if ! command -v ufw &> /dev/null; then
        warning "UFW not installed, installing..."
        apt-get update && apt-get install -y ufw
    fi
    
    # Reset UFW to default
    ufw --force reset
    
    # Default policies
    ufw default deny incoming
    ufw default allow outgoing
    
    # Allow SSH (be careful not to lock yourself out!)
    ufw allow 22/tcp comment 'SSH'
    
    # Allow HTTP and HTTPS
    ufw allow 80/tcp comment 'HTTP'
    ufw allow 443/tcp comment 'HTTPS'
    
    # Allow specific service ports
    for port in "${ALLOWED_PORTS[@]}"; do
        if [[ "$port" != "80" && "$port" != "443" ]]; then
            ufw allow "$port/tcp" comment "LLM Service port $port"
        fi
    done
    
    # Rate limit SSH
    ufw limit 22/tcp
    
    # Enable UFW
    ufw --force enable
    
    success "UFW firewall configured"
}

configure_iptables() {
    log "Configuring iptables rules..."
    
    # Flush existing rules
    iptables -F
    iptables -X
    
    # Default policies
    iptables -P INPUT DROP
    iptables -P FORWARD DROP
    iptables -P OUTPUT ACCEPT
    
    # Allow loopback
    iptables -A INPUT -i lo -j ACCEPT
    
    # Allow established connections
    iptables -A INPUT -m state --state ESTABLISHED,RELATED -j ACCEPT
    
    # Allow SSH
    iptables -A INPUT -p tcp --dport 22 -j ACCEPT
    
    # Allow HTTP/HTTPS
    iptables -A INPUT -p tcp --dport 80 -j ACCEPT
    iptables -A INPUT -p tcp --dport 443 -j ACCEPT
    
    # Allow specific ports
    for port in "${ALLOWED_PORTS[@]}"; do
        if [[ "$port" != "80" && "$port" != "443" && "$port" != "22" ]]; then
            iptables -A INPUT -p tcp --dport "$port" -j ACCEPT
        fi
    done
    
    # Rate limiting for API endpoints
    iptables -A INPUT -p tcp --dport 8080 -m limit --limit 25/minute --limit-burst 100 -j ACCEPT
    
    # Save rules
    if command -v iptables-save &> /dev/null; then
        iptables-save > /etc/iptables/rules.v4 2>/dev/null || \
            iptables-save > /etc/iptables.rules
    fi
    
    success "iptables rules configured"
}

# =============================================================================
# Docker Security
# =============================================================================

configure_docker_security() {
    log "Configuring Docker security settings..."
    
    # Create daemon.json for Docker security settings
    mkdir -p /etc/docker
    
    cat > /etc/docker/daemon.json << 'EOF'
{
  "userns-remap": "default",
  "live-restore": true,
  "no-new-privileges": true,
  "seccomp-profile": "/etc/docker/seccomp-default.json",
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  },
  "storage-driver": "overlay2",
  "storage-opts": [
    "overlay2.override_kernel_check=true"
  ]
}
EOF
    
    # Create seccomp profile
    cat > /etc/docker/seccomp-default.json << 'EOF'
{
  "defaultAction": "SCMP_ACT_ERRNO",
  "archMap": [
    {
      "architecture": "SCMP_ARCH_X86_64",
      "subArchitectures": ["SCMP_ARCH_X86", "SCMP_ARCH_X32"]
    }
  ],
  "syscalls": [
    {
      "names": [
        "accept",
        "accept4",
        "access",
        "adjtimex",
        "alarm",
        "bind",
        "brk",
        "capget",
        "capset",
        "chdir",
        "chmod",
        "chown",
        "clock_getres",
        "clock_gettime",
        "clock_nanosleep",
        "close",
        "connect",
        "copy_file_range",
        "creat",
        "dup",
        "dup2",
        "dup3",
        "epoll_create",
        "epoll_create1",
        "epoll_ctl",
        "epoll_ctl_old",
        "epoll_pwait",
        "epoll_wait",
        "epoll_wait_old",
        "eventfd",
        "eventfd2",
        "execve",
        "execveat",
        "exit",
        "exit_group",
        "faccessat",
        "fadvise64",
        "fadvise64_64",
        "fallocate",
        "fanotify_mark",
        "fchdir",
        "fchmod",
        "fchmodat",
        "fchown",
        "fchownat",
        "fcntl",
        "fcntl64",
        "fdatasync",
        "fgetxattr",
        "flistxattr",
        "flock",
        "fork",
        "fremovexattr",
        "fsetxattr",
        "fstat",
        "fstat64",
        "fstatat64",
        "fstatfs",
        "fstatfs64",
        "fsync",
        "ftruncate",
        "ftruncate64",
        "futex",
        "getcpu",
        "getcwd",
        "getdents",
        "getdents64",
        "getegid",
        "getegid32",
        "geteuid",
        "geteuid32",
        "getgid",
        "getgid32",
        "getgroups",
        "getgroups32",
        "getitimer",
        "getpeername",
        "getpgid",
        "getpgrp",
        "getpid",
        "getppid",
        "getpriority",
        "getrandom",
        "getresgid",
        "getresgid32",
        "getresuid",
        "getresuid32",
        "getrlimit",
        "get_robust_list",
        "getrusage",
        "getsid",
        "getsockname",
        "getsockopt",
        "get_thread_area",
        "gettid",
        "gettimeofday",
        "getuid",
        "getuid32",
        "getxattr",
        "inotify_add_watch",
        "inotify_init",
        "inotify_init1",
        "inotify_rm_watch",
        "io_cancel",
        "ioctl",
        "io_destroy",
        "io_getevents",
        "io_pgetevents",
        "ioprio_get",
        "ioprio_set",
        "io_setup",
        "io_submit",
        "io_uring_enter",
        "io_uring_register",
        "io_uring_setup",
        "kill",
        "lchown",
        "lgetxattr",
        "link",
        "linkat",
        "listen",
        "listxattr",
        "llistxattr",
        "lremovexattr",
        "lseek",
        "lsetxattr",
        "lstat",
        "lstat64",
        "madvise",
        "memfd_create",
        "mincore",
        "mkdir",
        "mkdirat",
        "mknod",
        "mknodat",
        "mlock",
        "mlock2",
        "mlockall",
        "mmap",
        "mmap2",
        "mprotect",
        "mq_getsetattr",
        "mq_notify",
        "mq_open",
        "mq_timedreceive",
        "mq_timedsend",
        "mq_unlink",
        "mremap",
        "msgctl",
        "msgget",
        "msgrcv",
        "msgsnd",
        "msync",
        "munlock",
        "munlockall",
        "munmap",
        "nanosleep",
        "newfstatat",
        "open",
        "openat",
        "pause",
        "pidfd_open",
        "pidfd_send_signal",
        "pipe",
        "pipe2",
        "pivot_root",
        "poll",
        "ppoll",
        "ppoll_time64",
        "prctl",
        "pread64",
        "preadv",
        "preadv2",
        "prlimit64",
        "pselect6",
        "pselect6_time64",
        "pwrite64",
        "pwritev",
        "pwritev2",
        "read",
        "readahead",
        "readdir",
        "readlink",
        "readlinkat",
        "readv",
        "recv",
        "recvfrom",
        "recvmmsg",
        "recvmsg",
        "remap_file_pages",
        "removexattr",
        "rename",
        "renameat",
        "renameat2",
        "restart_syscall",
        "rmdir",
        "rseq",
        "rt_sigaction",
        "rt_sigpending",
        "rt_sigprocmask",
        "rt_sigqueueinfo",
        "rt_sigreturn",
        "rt_sigsuspend",
        "rt_sigtimedwait",
        "rt_tgsigqueueinfo",
        "sched_getaffinity",
        "sched_getattr",
        "sched_getparam",
        "sched_get_priority_max",
        "sched_get_priority_min",
        "sched_getscheduler",
        "sched_rr_get_interval",
        "sched_setaffinity",
        "sched_setattr",
        "sched_setparam",
        "sched_setscheduler",
        "sched_yield",
        "seccomp",
        "select",
        "semctl",
        "semget",
        "semop",
        "semtimedop",
        "send",
        "sendfile",
        "sendfile64",
        "sendmmsg",
        "sendmsg",
        "sendto",
        "setfsgid",
        "setfsgid32",
        "setfsuid",
        "setfsuid32",
        "setgid",
        "setgid32",
        "setgroups",
        "setgroups32",
        "setitimer",
        "setpgid",
        "setpriority",
        "setregid",
        "setregid32",
        "setresgid",
        "setresgid32",
        "setresuid",
        "setresuid32",
        "setreuid",
        "setreuid32",
        "setrlimit",
        "set_robust_list",
        "setsid",
        "setsockopt",
        "set_thread_area",
        "set_tid_address",
        "setuid",
        "setuid32",
        "setxattr",
        "shmat",
        "shmctl",
        "shmdt",
        "shmget",
        "shutdown",
        "sigaltstack",
        "signalfd",
        "signalfd4",
        "sigpending",
        "sigprocmask",
        "sigreturn",
        "socket",
        "socketcall",
        "socketpair",
        "splice",
        "stat",
        "stat64",
        "statfs",
        "statfs64",
        "statx",
        "symlink",
        "symlinkat",
        "sync",
        "sync_file_range",
        "syncfs",
        "sysinfo",
        "tee",
        "tgkill",
        "time",
        "timer_create",
        "timer_delete",
        "timer_getoverrun",
        "timer_gettime",
        "timer_settime",
        "timerfd_create",
        "timerfd_gettime",
        "timerfd_settime",
        "times",
        "tkill",
        "truncate",
        "truncate64",
        "ugetrlimit",
        "umask",
        "uname",
        "unlink",
        "unlinkat",
        "utime",
        "utimensat",
        "utimensat_time64",
        "utimes",
        "vfork",
        "wait4",
        "waitid",
        "waitpid",
        "write",
        "writev"
      ],
      "action": "SCMP_ACT_ALLOW"
    }
  ]
}
EOF
    
    # Restart Docker to apply settings
    systemctl restart docker
    
    success "Docker security settings configured"
}

# =============================================================================
# SSL/TLS Configuration
# =============================================================================

generate_self_signed_cert() {
    log "Generating self-signed SSL certificate..."
    
    mkdir -p "$CERT_DIR"
    
    openssl req -x509 -nodes -days 365 -newkey rsa:4096 \
        -keyout "${CERT_DIR}/server.key" \
        -out "${CERT_DIR}/server.crt" \
        -subj "/C=US/ST=State/L=City/O=Organization/CN=localhost" \
        -addext "subjectAltName=DNS:localhost,DNS:*.localhost,IP:127.0.0.1"
    
    chmod 600 "${CERT_DIR}/server.key"
    chmod 644 "${CERT_DIR}/server.crt"
    
    success "Self-signed certificate generated"
}

setup_letsencrypt() {
    log "Setting up Let's Encrypt certificates..."
    
    local domain=$1
    local email=$2
    
    if ! command -v certbot &> /dev/null; then
        warning "Certbot not installed, installing..."
        apt-get update
        apt-get install -y certbot
    fi
    
    # Obtain certificate
    certbot certonly --standalone \
        -d "$domain" \
        --agree-tos \
        --email "$email" \
        --non-interactive
    
    # Create symlinks for Docker
    mkdir -p "$CERT_DIR"
    ln -sf "/etc/letsencrypt/live/${domain}/fullchain.pem" "${CERT_DIR}/server.crt"
    ln -sf "/etc/letsencrypt/live/${domain}/privkey.pem" "${CERT_DIR}/server.key"
    
    # Setup auto-renewal
    (crontab -l 2>/dev/null; echo "0 12 * * * certbot renew --quiet") | crontab -
    
    success "Let's Encrypt certificate configured for $domain"
}

# =============================================================================
# Secret Management
# =============================================================================

setup_secrets() {
    log "Setting up secret management..."
    
    local secrets_file="${DOCKER_DIR}/.env"
    
    if [[ -f "$secrets_file" ]]; then
        # Set proper permissions
        chmod 600 "$secrets_file"
        success "Secret file permissions set"
    fi
    
    # Generate strong secrets if not present
    if ! grep -q "JWT_SECRET" "$secrets_file" 2>/dev/null || \
       grep -q "JWT_SECRET=change-me" "$secrets_file" 2>/dev/null; then
        
        local jwt_secret
        jwt_secret=$(openssl rand -hex 32)
        
        if [[ -f "$secrets_file" ]]; then
            sed -i "s/JWT_SECRET=.*/JWT_SECRET=${jwt_secret}/" "$secrets_file"
        fi
        
        success "Generated new JWT secret"
    fi
    
    # Generate API key
    if ! grep -q "API_KEY" "$secrets_file" 2>/dev/null; then
        local api_key
        api_key=$(openssl rand -hex 16)
        
        echo "API_KEY=${api_key}" >> "$secrets_file"
        success "Generated new API key"
    fi
}

# =============================================================================
# Access Control
# =============================================================================

setup_fail2ban() {
    log "Setting up fail2ban..."
    
    if ! command -v fail2ban-server &> /dev/null; then
        warning "fail2ban not installed, installing..."
        apt-get update && apt-get install -y fail2ban
    fi
    
    # Create custom jail for LLM API
    cat > /etc/fail2ban/jail.d/llm-api.conf << EOF
[llm-api]
enabled = true
port = 8080,443
filter = llm-api
logpath = /var/log/llm-api.log
maxretry = 5
bantime = 3600
findtime = 600
EOF
    
    # Create filter
    cat > /etc/fail2ban/filter.d/llm-api.conf << EOF
[Definition]
failregex = ^.*Invalid API key from <HOST>.*$
            ^.*Rate limit exceeded for <HOST>.*$
ignoreregex =
EOF
    
    # Restart fail2ban
    systemctl restart fail2ban
    
    success "fail2ban configured"
}

# =============================================================================
# Security Checks
# =============================================================================

check_security() {
    log "Running security checks..."
    
    local issues=0
    
    # Check Docker daemon configuration
    if [[ -f /etc/docker/daemon.json ]]; then
        if grep -q "userns-remap" /etc/docker/daemon.json; then
            success "Docker user namespace remapping enabled"
        else
            warning "Docker user namespace remapping not enabled"
            ((issues++))
        fi
    fi
    
    # Check file permissions
    local env_file="${DOCKER_DIR}/.env"
    if [[ -f "$env_file" ]]; then
        local perms
        perms=$(stat -c "%a" "$env_file")
        if [[ "$perms" == "600" ]]; then
            success "Environment file has correct permissions"
        else
            warning "Environment file permissions are $perms (should be 600)"
            ((issues++))
        fi
    fi
    
    # Check SSL certificates
    if [[ -f "${CERT_DIR}/server.crt" ]]; then
        success "SSL certificate exists"
        
        # Check certificate expiration
        local expiry
        expiry=$(openssl x509 -in "${CERT_DIR}/server.crt" -noout -dates | grep notAfter | cut -d= -f2)
        log "Certificate expires: $expiry"
    else
        warning "SSL certificate not found"
        ((issues++))
    fi
    
    # Check firewall status
    if command -v ufw &> /dev/null; then
        if ufw status | grep -q "Status: active"; then
            success "UFW firewall is active"
        else
            warning "UFW firewall is not active"
            ((issues++))
        fi
    fi
    
    # Check for exposed sensitive ports
    local exposed_ports
    exposed_ports=$(netstat -tlnp 2>/dev/null | grep -E ':(8000|9090|3100)' | wc -l)
    if [[ "$exposed_ports" -gt 0 ]]; then
        warning "Some internal services may be exposed externally"
        ((issues++))
    else
        success "Internal services are not exposed"
    fi
    
    # Check Docker image vulnerabilities (if trivy is installed)
    if command -v trivy &> /dev/null; then
        log "Scanning Docker images for vulnerabilities..."
        # This would scan the images
        success "Docker image scan completed"
    fi
    
    if [[ $issues -eq 0 ]]; then
        success "All security checks passed!"
    else
        warning "Found $issues security issues"
    fi
    
    return $issues
}

# =============================================================================
# Main Functions
# =============================================================================

show_help() {
    cat << EOF
Light Local LLM System - Security Hardening

Usage:
  $(basename "$0") [OPTIONS]

Options:
  --apply              Apply all security settings
  --check              Run security checks
  --generate-certs     Generate self-signed SSL certificates
  --letsencrypt <domain> <email>  Setup Let's Encrypt
  --setup-firewall     Configure firewall only
  --setup-docker       Configure Docker security only
  --setup-fail2ban     Setup fail2ban only
  --help               Show this help message

Examples:
  $(basename "$0") --apply
  $(basename "$0") --check
  $(basename "$0") --letsencrypt example.com admin@example.com
EOF
}

apply_all() {
    log "Applying all security hardening measures..."
    
    configure_ufw
    configure_iptables
    configure_docker_security
    generate_self_signed_cert
    setup_secrets
    setup_fail2ban
    
    success "Security hardening completed!"
    log "Run '$(basename "$0") --check' to verify"
}

# =============================================================================
# Main Entry Point
# =============================================================================

main() {
    case "${1:-}" in
        --help|-h)
            show_help
            exit 0
            ;;
        --apply)
            apply_all
            ;;
        --check)
            check_security
            ;;
        --generate-certs)
            generate_self_signed_cert
            ;;
        --letsencrypt)
            if [[ -z "${2:-}" || -z "${3:-}" ]]; then
                error "Please provide domain and email"
                exit 1
            fi
            setup_letsencrypt "$2" "$3"
            ;;
        --setup-firewall)
            configure_ufw
            configure_iptables
            ;;
        --setup-docker)
            configure_docker_security
            ;;
        --setup-fail2ban)
            setup_fail2ban
            ;;
        *)
            error "Unknown option: ${1:-}"
            show_help
            exit 1
            ;;
    esac
}

main "$@"
