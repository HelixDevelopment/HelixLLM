# Phase 1: Infrastructure & RPC Cluster Setup Guide
## Distributed LLM System with llama.cpp

**Version:** 1.0  
**Last Updated:** 2025  
**Target Hardware:**
- **Machine 1 (Master Node - Laptop):** Lenovo ThinkBook 16 Pro, AMD Ryzen 9, 32GB DDR4 RAM, NVIDIA RTX GPU, 4TB NVMe SSD
- **Machine 2 (Worker Node - Desktop):** Intel i7 11th Gen, 64GB DDR4 RAM, 2x 2TB NVMe SSD
- **Network:** Wired Ethernet 1GbE preferred

---

## Table of Contents

1. [Prerequisites](#1-prerequisites)
2. [Network Setup](#2-network-setup)
3. [llama.cpp Installation](#3-llamacpp-installation)
4. [RPC Worker Node Setup](#4-rpc-worker-node-setup)
5. [RPC Master Node Setup](#5-rpc-master-node-setup)
6. [Performance Optimization](#6-performance-optimization)
7. [Verification & Testing](#7-verification--testing)
8. [Troubleshooting Guide](#8-troubleshooting-guide)

---

## 1. Prerequisites

### 1.1 Required Software (Both Machines)

| Software | Version | Purpose |
|----------|---------|---------|
| Windows 10/11 | 21H2+ | Operating System |
| NVIDIA GPU Driver | 545.84+ | GPU acceleration |
| CUDA Toolkit | 12.4+ | CUDA runtime |
| PowerShell | 5.1+ | Scripting |
| 7-Zip or WinRAR | Latest | Archive extraction |

### 1.2 Verify GPU Drivers

**On Both Machines (PowerShell as Administrator):**

```powershell
# Check NVIDIA GPU status
nvidia-smi
```

**Expected Output:**
```
+-----------------------------------------------------------------------------------------+
| NVIDIA-SMI 545.84                 Driver Version: 545.84       CUDA Version: 12.3     |
|-----------------------------------------+------------------------+----------------------+
| GPU  Name                 TCC/WDDM  | Bus-Id          Disp.A | Volatile Uncorr. ECC |
| Fan  Temp   Perf          Pwr:Usage/Cap|         Memory-Usage | GPU-Util  Compute M. |
|                                         |                      |               MIG M. |
|=========================================+======================+======================|
|   0  NVIDIA GeForce RTX 3060      WDDM  |   00000000:01:00.0  On|                  N/A |
|  0%   45C    P8              15W / 170W|    1024MiB / 12288MiB|      0%      Default |
+-----------------------------------------+------------------------+----------------------+
```

### 1.3 Verify CUDA Installation

```powershell
# Check CUDA version
nvcc --version
```

**Expected Output:**
```
nvcc: NVIDIA (R) Cuda compiler driver
Copyright (c) 2005-2023 NVIDIA Corporation
Built on Wed_Nov_22_10:30:42_Pacific_Standard_Time_2023
Cuda compilation tools, release 12.3, V12.3.107
```

---

## 2. Network Setup

### 2.1 Network Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                    Network Topology                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────────┐         ┌─────────────────────┐        │
│  │   Machine 1         │         │   Machine 2         │        │
│  │   (Master Node)     │◄───────►│   (Worker Node)     │        │
│  │   192.168.1.100     │  1GbE   │   192.168.1.101     │        │
│  │   Port: 8080        │         │   Port: 50052       │        │
│  └─────────────────────┘         └─────────────────────┘        │
│           │                               │                      │
│           │                               │                      │
│           ▼                               ▼                      │
│    ┌──────────────┐              ┌──────────────┐               │
│    │ llama-server │              │ rpc-server   │               │
│    │ (API Server) │              │ (Compute)    │               │
│    └──────────────┘              └──────────────┘               │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 Static IP Configuration (Windows)

#### Machine 1 (Master Node - Laptop)

**Option A: PowerShell Command (Recommended)**

```powershell
# Run PowerShell as Administrator
# Get the network adapter name
Get-NetAdapter | Where-Object {$_.Status -eq "Up"}

# Set static IP (replace "Ethernet" with your adapter name if different)
New-NetIPAddress -InterfaceAlias "Ethernet" -IPAddress 192.168.1.100 -PrefixLength 24 -DefaultGateway 192.168.1.1

# Set DNS servers
Set-DnsClientServerAddress -InterfaceAlias "Ethernet" -ServerAddresses 8.8.8.8,8.8.4.4

# Verify configuration
Get-NetIPAddress -InterfaceAlias "Ethernet" | Where-Object {$_.AddressFamily -eq "IPv4"}
```

**Option B: Netsh Command**

```powershell
# Run as Administrator
netsh interface ip set address name="Ethernet" static 192.168.1.100 255.255.255.0 192.168.1.1
netsh interface ip set dns name="Ethernet" static 8.8.8.8
netsh interface ip add dns name="Ethernet" 8.8.4.4 index=2
```

#### Machine 2 (Worker Node - Desktop)

```powershell
# Run PowerShell as Administrator
# Set static IP
New-NetIPAddress -InterfaceAlias "Ethernet" -IPAddress 192.168.1.101 -PrefixLength 24 -DefaultGateway 192.168.1.1

# Set DNS servers
Set-DnsClientServerAddress -InterfaceAlias "Ethernet" -ServerAddresses 8.8.8.8,8.8.4.4

# Verify configuration
Get-NetIPAddress -InterfaceAlias "Ethernet" | Where-Object {$_.AddressFamily -eq "IPv4"}
```

### 2.3 Windows Firewall Configuration

#### Machine 1 (Master Node) - PowerShell Commands

```powershell
# Run PowerShell as Administrator

# Create firewall rule for llama-server API (port 8080)
New-NetFirewallRule -DisplayName "LLaMA Server API" -Direction Inbound -LocalPort 8080 -Protocol TCP -Action Allow

# Create firewall rule for RPC client connections (outbound to worker)
New-NetFirewallRule -DisplayName "LLaMA RPC Client" -Direction Outbound -RemotePort 50052 -Protocol TCP -Action Allow

# Allow ICMP for ping testing
New-NetFirewallRule -DisplayName "ICMPv4-In" -Protocol ICMPv4 -IcmpType 8 -Direction Inbound -Action Allow

# Verify rules
Get-NetFirewallRule | Where-Object {$_.DisplayName -like "*LLaMA*"} | Format-Table DisplayName, Enabled, Direction, Action
```

#### Machine 2 (Worker Node) - PowerShell Commands

```powershell
# Run PowerShell as Administrator

# Create firewall rule for RPC server (port 50052)
New-NetFirewallRule -DisplayName "LLaMA RPC Server" -Direction Inbound -LocalPort 50052 -Protocol TCP -Action Allow

# Create firewall rule for RPC server (outbound responses)
New-NetFirewallRule -DisplayName "LLaMA RPC Server Out" -Direction Outbound -LocalPort 50052 -Protocol TCP -Action Allow

# Allow ICMP for ping testing
New-NetFirewallRule -DisplayName "ICMPv4-In" -Protocol ICMPv4 -IcmpType 8 -Direction Inbound -Action Allow

# Verify rules
Get-NetFirewallRule | Where-Object {$_.DisplayName -like "*LLaMA*"} | Format-Table DisplayName, Enabled, Direction, Action
```

### 2.4 Network Connectivity Testing

#### From Machine 1 (Master):

```powershell
# Test connectivity to worker node
ping 192.168.1.101

# Test RPC port connectivity
tnc 192.168.1.101 -Port 50052

# Expected output for successful connection:
# ComputerName     : 192.168.1.101
# RemoteAddress    : 192.168.1.101
# RemotePort       : 50052
# InterfaceAlias   : Ethernet
# SourceAddress    : 192.168.1.100
# TcpTestSucceeded : True
```

#### From Machine 2 (Worker):

```powershell
# Test connectivity to master node
ping 192.168.1.100

# Verify local port is listening
Get-NetTCPConnection -LocalPort 50052 -ErrorAction SilentlyContinue
```

### 2.5 Network Performance Testing

```powershell
# On Machine 2 (Worker) - Install iperf3 if needed
# Download from: https://iperf.fr/iperf-download.php

# Start iperf3 server on worker
iperf3.exe -s -p 5201

# On Machine 1 (Master) - Test bandwidth
iperf3.exe -c 192.168.1.101 -p 5201 -t 30

# Expected: >900 Mbps for 1GbE network
```

---

## 3. llama.cpp Installation

### 3.1 Download Pre-built Windows Binaries

#### Latest Release URLs (CUDA 12.4)

| Component | Download URL | Size |
|-----------|--------------|------|
| llama.cpp binaries | https://github.com/ggml-org/llama.cpp/releases/download/b7509/llama-b7509-bin-win-cuda-12.4-x64.zip | ~15 MB |
| CUDA runtime DLLs | https://github.com/ggml-org/llama.cpp/releases/download/b7509/cudart-llama-bin-win-cuda-12.4-x64.zip | ~50 MB |

#### Alternative: Latest Stable Release

```powershell
# PowerShell download commands (Run on BOTH machines)

# Create installation directory
New-Item -ItemType Directory -Force -Path "C:\llama.cpp"
Set-Location -Path "C:\llama.cpp"

# Download llama.cpp binaries
Invoke-WebRequest -Uri "https://github.com/ggml-org/llama.cpp/releases/download/b7509/llama-b7509-bin-win-cuda-12.4-x64.zip" -OutFile "llama-bin.zip"

# Download CUDA runtime DLLs
Invoke-WebRequest -Uri "https://github.com/ggml-org/llama.cpp/releases/download/b7509/cudart-llama-bin-win-cuda-12.4-x64.zip" -OutFile "cudart.zip"

# Extract archives
Expand-Archive -Path "llama-bin.zip" -DestinationPath "." -Force
Expand-Archive -Path "cudart.zip" -DestinationPath "." -Force

# Verify extraction
Get-ChildItem -Path "C:\llama.cpp" -Recurse | Where-Object {$_.Name -like "*.exe"}
```

### 3.2 Directory Structure

**Recommended Directory Layout (Both Machines):**

```
C:\llama.cpp\
├── bin\
│   ├── llama-cli.exe
│   ├── llama-server.exe
│   ├── rpc-server.exe
│   ├── llama-bench.exe
│   └── *.dll (CUDA runtime)
├── models\
│   └── (downloaded GGUF models)
├── cache\
│   └── (RPC cache directory)
└── logs\
    └── (server logs)
```

**Create Directory Structure:**

```powershell
# Run on BOTH machines
New-Item -ItemType Directory -Force -Path "C:\llama.cpp\models"
New-Item -ItemType Directory -Force -Path "C:\llama.cpp\cache"
New-Item -ItemType Directory -Force -Path "C:\llama.cpp\logs"
```

### 3.3 Environment Variable Setup

```powershell
# Run PowerShell as Administrator on BOTH machines

# Add llama.cpp to PATH
[Environment]::SetEnvironmentVariable("Path", $env:Path + ";C:\llama.cpp\bin", "Machine")

# Set CUDA environment variables (if not already set)
[Environment]::SetEnvironmentVariable("CUDA_PATH", "C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v12.4", "Machine")
[Environment]::SetEnvironmentVariable("CUDA_HOME", "C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v12.4", "Machine")

# Set llama.cpp cache directory for RPC
[Environment]::SetEnvironmentVariable("LLAMA_CACHE", "C:\llama.cpp\cache", "User")

# Verify environment variables
Get-ChildItem Env: | Where-Object {$_.Name -like "*CUDA*" -or $_.Name -like "*LLAMA*"}

# Reload environment variables
$env:Path = [Environment]::GetEnvironmentVariable("Path", "Machine")
```

### 3.4 Verify Installation

```powershell
# Open new PowerShell window and verify
Set-Location -Path "C:\llama.cpp\bin"

# Check llama-server version
.\llama-server.exe --version

# Expected output:
# version: 3759 (b7509)
# built with MSVC 19.29.30157.0

# Check rpc-server
.\rpc-server.exe --help | Select-Object -First 20
```

---

## 4. RPC Worker Node Setup

### 4.1 Worker Node Configuration (Machine 2 - Desktop)

#### Hardware-Specific Settings

| Parameter | Value | Description |
|-----------|-------|-------------|
| CPU | Intel i7 11th Gen | 8 cores / 16 threads |
| RAM | 64GB DDR4 | Available for model caching |
| Storage | 2x 2TB NVMe | Cache and swap space |
| Cache Size | 32GB | Recommended for large models |

#### RPC Server Startup Command

```powershell
# Navigate to llama.cpp directory
Set-Location -Path "C:\llama.cpp\bin"

# Start RPC server with full configuration
.\rpc-server.exe `
    --host 0.0.0.0 `
    --port 50052 `
    --cache "C:\llama.cpp\cache" `
    --threads 16
```

#### Create Startup Script (Worker Node)

Create file: `C:\llama.cpp\start-rpc-server.ps1`

```powershell
# start-rpc-server.ps1
# RPC Worker Node Startup Script

$ErrorActionPreference = "Stop"

# Configuration
$llamaPath = "C:\llama.cpp\bin"
$cachePath = "C:\llama.cpp\cache"
$logPath = "C:\llama.cpp\logs"
$port = 50052
$threads = 16

# Create log directory if not exists
New-Item -ItemType Directory -Force -Path $logPath | Out-Null

# Generate log filename with timestamp
$timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
$logFile = Join-Path $logPath "rpc-server_$timestamp.log"

Write-Host "Starting RPC Server..." -ForegroundColor Green
Write-Host "Port: $port" -ForegroundColor Cyan
Write-Host "Threads: $threads" -ForegroundColor Cyan
Write-Host "Cache: $cachePath" -ForegroundColor Cyan
Write-Host "Log: $logFile" -ForegroundColor Cyan
Write-Host ""

# Start RPC server
Set-Location -Path $llamaPath

# Start with logging
Start-Process -FilePath ".\rpc-server.exe" `
    -ArgumentList "--host", "0.0.0.0", `
                  "--port", $port, `
                  "--cache", $cachePath, `
                  "--threads", $threads `
    -RedirectStandardOutput $logFile `
    -RedirectStandardError $logFile `
    -WindowStyle Normal

Write-Host "RPC Server started. Monitoring log file..." -ForegroundColor Green
Get-Content -Path $logFile -Wait -Tail 10
```

#### Alternative: Batch File for Easy Startup

Create file: `C:\llama.cpp\start-rpc-server.bat`

```batch
@echo off
setlocal EnableDelayedExpansion

echo ==========================================
echo    LLaMA RPC Server - Worker Node
echo ==========================================
echo.

cd /d "C:\llama.cpp\bin"

echo Starting RPC Server on port 50052...
echo Cache directory: C:\llama.cpp\cache
echo.

rpc-server.exe --host 0.0.0.0 --port 50052 --cache "C:\llama.cpp\cache" --threads 16

echo.
echo RPC Server stopped.
pause
```

### 4.2 RPC Server Parameters Reference

| Parameter | Short | Default | Description |
|-----------|-------|---------|-------------|
| `--host` | | 0.0.0.0 | Bind address for RPC server |
| `--port` | `-p` | 50052 | Port to listen on |
| `--cache` | `-c` | ~/.cache/llama.cpp/rpc | Local tensor cache directory |
| `--threads` | `-t` | Auto | Number of CPU threads |

### 4.3 Verify RPC Server is Running

```powershell
# Check if RPC server is listening
Get-NetTCPConnection -LocalPort 50052 | Select-Object LocalAddress, LocalPort, State, OwningProcess

# Check process
Get-Process | Where-Object {$_.ProcessName -like "*rpc*"}

# View RPC server logs
tail -f C:\llama.cpp\logs\rpc-server_*.log

# Test with telnet (if installed)
telnet localhost 50052
```

**Expected Output (Successful Start):**
```
create_backend: using CUDA backend
ggml_cuda_init: GGML_CUDA_FORCE_MMQ:   no
ggml_cuda_init: CUDA_USE_TENSOR_CORES: yes
ggml_cuda_init: found 1 CUDA devices:
  Device 0: NVIDIA GeForce RTX 3060, compute capability 8.6, VMM: yes
Starting RPC server on 0.0.0.0:50052
```

### 4.4 Multiple GPU Support (If Applicable)

```powershell
# For multi-GPU systems, specify device with environment variable
$env:CUDA_VISIBLE_DEVICES = "0"
.\rpc-server.exe --port 50052

# Second GPU on different port
$env:CUDA_VISIBLE_DEVICES = "1"
.\rpc-server.exe --port 50053
```

---

## 5. RPC Master Node Setup

### 5.1 Model Download URLs and Recommendations

#### Recommended Models for Your Hardware

| Model | Size (Q4_K_M) | VRAM Required | Use Case | Download URL |
|-------|---------------|---------------|----------|--------------|
| **Qwen2.5-14B-Instruct** | ~9GB | ~12GB | Balanced performance | [HuggingFace](https://huggingface.co/Qwen/Qwen2.5-14B-Instruct-GGUF) |
| **Llama-3.1-70B-Instruct** | ~38GB | ~42GB | **RECOMMENDED** - Best quality | [HuggingFace](https://huggingface.co/meta-llama/Llama-3.1-70B-Instruct-GGUF) |
| **Mixtral-8x22B-Instruct** | ~67GB | ~72GB | Maximum capability | [HuggingFace](https://huggingface.co/mistralai/Mixtral-8x22B-Instruct-v0.1-GGUF) |

#### Model Download Commands

**Option 1: Using huggingface-cli (Recommended)**

```powershell
# Install huggingface-hub if not already installed
pip install huggingface-hub

# Download Qwen2.5-14B-Instruct (Q4_K_M)
huggingface-cli download Qwen/Qwen2.5-14B-Instruct-GGUF --include "*Q4_K_M.gguf" --local-dir "C:\llama.cpp\models\Qwen2.5-14B"

# Download Llama-3.1-70B-Instruct (Q4_K_M) - RECOMMENDED
huggingface-cli download meta-llama/Llama-3.1-70B-Instruct-GGUF --include "*Q4_K_M.gguf" --local-dir "C:\llama.cpp\models\Llama-3.1-70B"

# Download Mixtral-8x22B-Instruct (Q4_K_M)
huggingface-cli download mistralai/Mixtral-8x22B-Instruct-v0.1-GGUF --include "*Q4_K_M.gguf" --local-dir "C:\llama.cpp\models\Mixtral-8x22B"
```

**Option 2: Direct Download with PowerShell**

```powershell
# Create models directory
New-Item -ItemType Directory -Force -Path "C:\llama.cpp\models\Llama-3.1-70B"

# Download Llama-3.1-70B-Instruct Q4_K_M (RECOMMENDED)
$modelUrl = "https://huggingface.co/meta-llama/Llama-3.1-70B-Instruct-GGUF/resolve/main/llama-3.1-70b-instruct-q4_k_m.gguf"
$outputPath = "C:\llama.cpp\models\Llama-3.1-70B\llama-3.1-70b-instruct-q4_k_m.gguf"

Invoke-WebRequest -Uri $modelUrl -OutFile $outputPath

# Verify download
Get-Item $outputPath | Select-Object Name, @{Name="SizeGB";Expression={[math]::Round($_.Length/1GB,2)}}
```

**Option 3: Using aria2 for Faster Downloads**

```powershell
# Install aria2 if needed: winget install aria2.aria2

# Download with aria2 (multi-connection)
aria2c.exe -x 4 -s 4 -d "C:\llama.cpp\models\Llama-3.1-70B" `
    "https://huggingface.co/meta-llama/Llama-3.1-70B-Instruct-GGUF/resolve/main/llama-3.1-70b-instruct-q4_k_m.gguf"
```

### 5.2 Master Node Configuration (Machine 1 - Laptop)

#### Hardware-Specific Settings

| Parameter | Value | Description |
|-----------|-------|-------------|
| CPU | AMD Ryzen 9 | 8+ cores / 16+ threads |
| RAM | 32GB DDR4 | Local model layers + context |
| GPU | NVIDIA RTX | Primary compute device |
| Context Size | 8192 | Recommended for 32GB RAM |

### 5.3 llama-server.exe Command with RPC

#### Basic RPC Configuration

```powershell
# Navigate to llama.cpp directory
Set-Location -Path "C:\llama.cpp\bin"

# Start llama-server with RPC worker
.\llama-server.exe `
    --model "C:\llama.cpp\models\Llama-3.1-70B\llama-3.1-70b-instruct-q4_k_m.gguf" `
    --rpc "192.168.1.101:50052" `
    --ctx-size 8192 `
    --ngl 99 `
    --host 0.0.0.0 `
    --port 8080
```

#### Full Production Configuration

```powershell
# Production-ready llama-server with all optimizations
.\llama-server.exe `
    --model "C:\llama.cpp\models\Llama-3.1-70B\llama-3.1-70b-instruct-q4_k_m.gguf" `
    --rpc "192.168.1.101:50052" `
    --ctx-size 8192 `
    --ngl 99 `
    --batch-size 2048 `
    --ubatch-size 512 `
    --threads 8 `
    --threads-batch 16 `
    --flash-attn `
    --host 0.0.0.0 `
    --port 8080 `
    --timeout 600 `
    --metrics
```

### 5.4 Create Master Node Startup Script

Create file: `C:\llama.cpp\start-llama-server.ps1`

```powershell
# start-llama-server.ps1
# LLaMA Server Master Node Startup Script with RPC

$ErrorActionPreference = "Stop"

# Configuration
$llamaPath = "C:\llama.cpp\bin"
$modelPath = "C:\llama.cpp\models\Llama-3.1-70B\llama-3.1-70b-instruct-q4_k_m.gguf"
$logPath = "C:\llama.cpp\logs"
$rpcEndpoint = "192.168.1.101:50052"
$apiPort = 8080
$ctxSize = 8192
$gpuLayers = 99

# Verify model exists
if (-not (Test-Path $modelPath)) {
    Write-Error "Model not found: $modelPath"
    exit 1
}

# Create log directory
New-Item -ItemType Directory -Force -Path $logPath | Out-Null

# Generate log filename
$timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
$logFile = Join-Path $logPath "llama-server_$timestamp.log"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "   LLaMA Server - Master Node" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Configuration:" -ForegroundColor Yellow
Write-Host "  Model: $modelPath" -ForegroundColor White
Write-Host "  RPC Endpoint: $rpcEndpoint" -ForegroundColor White
Write-Host "  API Port: $apiPort" -ForegroundColor White
Write-Host "  Context Size: $ctxSize" -ForegroundColor White
Write-Host "  GPU Layers: $gpuLayers" -ForegroundColor White
Write-Host "  Log File: $logFile" -ForegroundColor White
Write-Host ""

Set-Location -Path $llamaPath

# Build argument list
$arguments = @(
    "--model", $modelPath,
    "--rpc", $rpcEndpoint,
    "--ctx-size", $ctxSize,
    "--ngl", $gpuLayers,
    "--batch-size", "2048",
    "--ubatch-size", "512",
    "--threads", "8",
    "--threads-batch", "16",
    "--flash-attn",
    "--host", "0.0.0.0",
    "--port", $apiPort,
    "--timeout", "600",
    "--metrics"
)

Write-Host "Starting llama-server..." -ForegroundColor Green
Write-Host ""

# Start server with output to console and log
& .\llama-server.exe @arguments *>&1 | Tee-Object -FilePath $logFile
```

### 5.5 llama-server Parameters Reference

| Parameter | Short | Default | Description |
|-----------|-------|---------|-------------|
| `--model` | `-m` | (required) | Path to GGUF model file |
| `--rpc` | | (none) | RPC server endpoint(s) |
| `--ctx-size` | `-c` | 0 (model default) | Context window size |
| `--ngl` | | 0 | GPU layers to offload (-1=all, 99=max) |
| `--batch-size` | `-b` | 2048 | Logical batch size |
| `--ubatch-size` | `-ub` | 512 | Physical batch size |
| `--threads` | `-t` | CPU cores/2 | CPU threads for generation |
| `--threads-batch` | `-tb` | CPU cores | CPU threads for batch processing |
| `--flash-attn` | `-fa` | off | Enable Flash Attention |
| `--host` | | 127.0.0.1 | API server bind address |
| `--port` | | 8080 | API server port |
| `--timeout` | | 600 | Request timeout in seconds |
| `--metrics` | | off | Enable Prometheus metrics |

### 5.6 Environment Variables for RPC

```powershell
# Set RPC connection timeout (seconds)
$env:GGML_RPC_TIMEOUT = "300"

# Set RPC connection retries
$env:GGML_RPC_RETRIES = "3"

# Enable verbose RPC logging
$env:GGML_RPC_VERBOSE = "1"
```

---

## 6. Performance Optimization

### 6.1 Tensor Split Configuration

For multi-GPU setups on the master node:

```powershell
# Split layers across multiple local GPUs
.\llama-server.exe `
    --model "C:\llama.cpp\models\model.gguf" `
    --tensor-split "0.6,0.4" `  # 60% on GPU 0, 40% on GPU 1
    --rpc "192.168.1.101:50052" `
    --ngl 99
```

### 6.2 Batch Size Tuning

| Scenario | batch-size | ubatch-size | Description |
|----------|------------|-------------|-------------|
| Single user | 512 | 256 | Low latency |
| Multi-user | 2048 | 512 | Balanced |
| High throughput | 4096 | 1024 | Maximum batching |

```powershell
# High-throughput configuration
.\llama-server.exe `
    --model "model.gguf" `
    --rpc "192.168.1.101:50052" `
    --batch-size 4096 `
    --ubatch-size 1024 `
    --parallel 4  # Enable parallel sequences
```

### 6.3 KV Cache Distribution

```powershell
# Optimize KV cache for distributed setup
.\llama-server.exe `
    --model "model.gguf" `
    --rpc "192.168.1.101:50052" `
    --ctx-size 8192 `
    --cache-type-k q8_0 `  # Quantized KV cache for keys
    --cache-type-v q8_0 `  # Quantized KV cache for values
    --flash-attn           # Flash Attention for efficiency
```

### 6.4 Memory Optimization

```powershell
# For limited VRAM scenarios
.\llama-server.exe `
    --model "model.gguf" `
    --rpc "192.168.1.101:50052" `
    --ngl 50 `           # Offload only 50 layers to GPU
    --ctx-size 4096 `    # Smaller context
    --no-mmap `          # Disable memory mapping
    --mlock              # Lock pages in memory
```

### 6.5 Performance Benchmarking

```powershell
# Run built-in benchmark
.\llama-bench.exe `
    -m "C:\llama.cpp\models\Llama-3.1-70B\llama-3.1-70b-instruct-q4_k_m.gguf" `
    --rpc "192.168.1.101:50052" `
    -p 512,1024,2048 `
    -n 128,256 `
    -ngl 99
```

---

## 7. Verification & Testing

### 7.1 Verify RPC Connection

#### On Master Node (Machine 1):

```powershell
# Check if llama-server is running
Get-Process | Where-Object {$_.ProcessName -like "*llama*"}

# Check if API port is listening
Get-NetTCPConnection -LocalPort 8080 | Select-Object LocalAddress, LocalPort, State

# Test API endpoint
Invoke-RestMethod -Uri "http://localhost:8080/health" -Method GET
```

#### Expected Output (Healthy):
```json
{
  "status": "ok",
  "model": "llama-3.1-70b-instruct-q4_k_m.gguf",
  "rpc_servers": ["192.168.1.101:50052"]
}
```

### 7.2 API Health Check Commands

```powershell
# Test server health
$health = Invoke-RestMethod -Uri "http://localhost:8080/health" -Method GET
Write-Host "Server Status: $($health.status)" -ForegroundColor Green

# Get model info
$props = Invoke-RestMethod -Uri "http://localhost:8080/props" -Method GET
Write-Host "Model: $($props.model)" -ForegroundColor Cyan
Write-Host "Context Size: $($props.n_ctx)" -ForegroundColor Cyan

# List available models
$models = Invoke-RestMethod -Uri "http://localhost:8080/v1/models" -Method GET
$models.data | ForEach-Object { Write-Host "Model: $($_.id)" -ForegroundColor Yellow }
```

### 7.3 Test Completion Endpoint

```powershell
# Simple completion test
$body = @{
    prompt = "Hello, my name is"
    n_predict = 50
    temperature = 0.7
} | ConvertTo-Json

$response = Invoke-RestMethod -Uri "http://localhost:8080/completion" -Method POST -Body $body -ContentType "application/json"
Write-Host "Generated Text: $($response.content)" -ForegroundColor Green
```

### 7.4 Test Chat Completion (OpenAI Compatible)

```powershell
# Chat completion test
$body = @{
    model = "llama-3.1-70b-instruct"
    messages = @(
        @{ role = "system"; content = "You are a helpful assistant." },
        @{ role = "user"; content = "What is the capital of France?" }
    )
    temperature = 0.7
    max_tokens = 100
} | ConvertTo-Json -Depth 10

$response = Invoke-RestMethod -Uri "http://localhost:8080/v1/chat/completions" -Method POST -Body $body -ContentType "application/json"
Write-Host "Response: $($response.choices[0].message.content)" -ForegroundColor Green
```

### 7.5 Performance Monitoring

```powershell
# Monitor GPU usage during inference
while ($true) {
    Clear-Host
    nvidia-smi
    Start-Sleep -Seconds 2
}
```

### 7.6 Log Analysis

```powershell
# View recent server logs
Get-Content -Path "C:\llama.cpp\logs\llama-server_*.log" -Tail 50

# Search for errors in logs
Select-String -Path "C:\llama.cpp\logs\llama-server_*.log" -Pattern "ERROR|error|Error" | Select-Object -Last 10

# Check RPC connection logs
Select-String -Path "C:\llama.cpp\logs\llama-server_*.log" -Pattern "RPC|rpc" | Select-Object -Last 10
```

---

## 8. Troubleshooting Guide

### 8.1 Common Errors and Solutions

#### Error: "Failed to connect to RPC server"

**Symptoms:**
```
error: failed to connect to RPC server 192.168.1.101:50052
```

**Solutions:**
```powershell
# 1. Verify worker node is running RPC server
Get-Process -ComputerName 192.168.1.101 | Where-Object {$_.ProcessName -like "*rpc*"}

# 2. Test network connectivity
tnc 192.168.1.101 -Port 50052

# 3. Check firewall rules on worker
Get-NetFirewallRule | Where-Object {$_.DisplayName -like "*RPC*"}

# 4. Restart RPC server on worker
# (Run on Machine 2)
Stop-Process -Name "rpc-server" -Force
Start-Process -FilePath "C:\llama.cpp\bin\rpc-server.exe" -ArgumentList "--port", "50052"
```

#### Error: "CUDA out of memory"

**Symptoms:**
```
CUDA error: out of memory
ggml_cuda_init: failed to initialize CUDA
```

**Solutions:**
```powershell
# 1. Reduce GPU layers
.\llama-server.exe --model "model.gguf" --rpc "192.168.1.101:50052" --ngl 50

# 2. Reduce context size
.\llama-server.exe --model "model.gguf" --rpc "192.168.1.101:50052" --ctx-size 4096

# 3. Use memory mapping
.\llama-server.exe --model "model.gguf" --rpc "192.168.1.101:50052" --mmap

# 4. Check GPU memory
nvidia-smi --query-gpu=memory.used,memory.free --format=csv
```

#### Error: "Model file not found"

**Symptoms:**
```
error: failed to load model: model.gguf
```

**Solutions:**
```powershell
# 1. Verify model path
Test-Path "C:\llama.cpp\models\Llama-3.1-70B\llama-3.1-70b-instruct-q4_k_m.gguf"

# 2. List available models
Get-ChildItem -Path "C:\llama.cpp\models" -Recurse -Filter "*.gguf"

# 3. Check file permissions
Get-Acl "C:\llama.cpp\models\model.gguf"
```

#### Error: "Port already in use"

**Symptoms:**
```
error: couldn't bind socket to address: Address already in use
```

**Solutions:**
```powershell
# 1. Find process using port
Get-NetTCPConnection -LocalPort 8080 | Select-Object OwningProcess
Get-Process -Id <PID>

# 2. Kill process using port
Stop-Process -Id <PID> -Force

# 3. Use different port
.\llama-server.exe --port 8081
```

### 8.2 Network Connectivity Issues

```powershell
# Test basic connectivity
ping 192.168.1.101

# Test port connectivity
tnc 192.168.1.101 -Port 50052

# Check routing
tracert 192.168.1.101

# Verify IP configuration
ipconfig /all

# Reset network stack (if needed)
netsh winsock reset
netsh int ip reset
Restart-Computer
```

### 8.3 Memory Allocation Problems

```powershell
# Check available system memory
Get-CimInstance -ClassName Win32_OperatingSystem | Select-Object TotalVisibleMemorySize, FreePhysicalMemory

# Check page file settings
Get-CimInstance -ClassName Win32_PageFileUsage

# Increase page file size (if needed)
wmic computersystem where name="%computername%" set AutomaticManagedPagefile=false
wmic pagefileset where name="C:\\pagefile.sys" set InitialSize=32768,MaximumSize=65536

# Check for memory leaks
Get-Process | Sort-Object WorkingSet -Descending | Select-Object -First 10 Name, WorkingSet
```

### 8.4 Performance Issues

```powershell
# Monitor CPU usage
Get-Counter '\Processor(_Total)\% Processor Time' -SampleInterval 2 -MaxSamples 10

# Monitor disk activity
Get-Counter '\PhysicalDisk(_Total)\% Disk Time' -SampleInterval 2 -MaxSamples 10

# Check network latency
ping 192.168.1.101 -t

# Profile RPC performance
# Add to llama-server startup:
# --metrics flag for Prometheus metrics
```

### 8.5 Windows Service Setup (Optional)

Create automatic startup services for production deployment:

```powershell
# Run as Administrator

# Create RPC Server service (Worker Node)
New-Service `
    -Name "LlamaRPCServer" `
    -DisplayName "LLaMA RPC Server" `
    -Description "Distributed LLM RPC Worker Node" `
    -BinaryPathName "C:\llama.cpp\bin\rpc-server.exe --port 50052 --cache C:\llama.cpp\cache" `
    -StartupType Automatic `
    -Credential (Get-Credential -Message "Enter service account credentials")

# Create LLaMA Server service (Master Node)
New-Service `
    -Name "LlamaAPIServer" `
    -DisplayName "LLaMA API Server" `
    -Description "Distributed LLM API Server with RPC" `
    -BinaryPathName "C:\llama.cpp\bin\llama-server.exe --model C:\llama.cpp\models\model.gguf --rpc 192.168.1.101:50052 --port 8080" `
    -StartupType Automatic

# Start services
Start-Service -Name "LlamaRPCServer"
Start-Service -Name "LlamaAPIServer"

# Verify services
Get-Service | Where-Object {$_.Name -like "*Llama*"}
```

### 8.6 Diagnostic Script

Create file: `C:\llama.cpp\diagnose.ps1`

```powershell
# diagnose.ps1
# Diagnostic script for LLaMA RPC cluster

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "   LLaMA RPC Cluster Diagnostics" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# System Information
Write-Host "=== System Information ===" -ForegroundColor Yellow
Get-ComputerInfo | Select-Object WindowsProductName, WindowsVersion, TotalPhysicalMemory, CsProcessors | Format-List

# GPU Information
Write-Host "=== GPU Information ===" -ForegroundColor Yellow
nvidia-smi

# Network Configuration
Write-Host "=== Network Configuration ===" -ForegroundColor Yellow
Get-NetIPAddress | Where-Object {$_.AddressFamily -eq "IPv4" -and $_.IPAddress -notlike "127.*"} | Format-Table InterfaceAlias, IPAddress, PrefixLength

# Firewall Rules
Write-Host "=== Firewall Rules ===" -ForegroundColor Yellow
Get-NetFirewallRule | Where-Object {$_.DisplayName -like "*LLaMA*" -or $_.DisplayName -like "*RPC*"} | Format-Table DisplayName, Enabled, Direction, Action

# Process Status
Write-Host "=== Process Status ===" -ForegroundColor Yellow
Get-Process | Where-Object {$_.ProcessName -like "*llama*" -or $_.ProcessName -like "*rpc*"} | Format-Table Name, Id, CPU, WorkingSet

# Port Status
Write-Host "=== Port Status ===" -ForegroundColor Yellow
Get-NetTCPConnection -LocalPort 50052,8080 -ErrorAction SilentlyContinue | Format-Table LocalAddress, LocalPort, RemoteAddress, State

# Test RPC Connection
Write-Host "=== RPC Connection Test ===" -ForegroundColor Yellow
tnc 192.168.1.101 -Port 50052 | Format-List

# API Health Check
Write-Host "=== API Health Check ===" -ForegroundColor Yellow
try {
    $health = Invoke-RestMethod -Uri "http://localhost:8080/health" -Method GET -TimeoutSec 5
    Write-Host "API Status: $($health.status)" -ForegroundColor Green
} catch {
    Write-Host "API Health Check Failed: $_" -ForegroundColor Red
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "   Diagnostics Complete" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
```

---

## Appendix A: Quick Reference Card

### Startup Commands

| Component | Command |
|-----------|---------|
| RPC Server (Worker) | `rpc-server.exe --port 50052 --cache C:\llama.cpp\cache` |
| LLaMA Server (Master) | `llama-server.exe --model model.gguf --rpc 192.168.1.101:50052 --ngl 99` |

### Key Ports

| Port | Service | Machine |
|------|---------|---------|
| 50052 | RPC Server | Worker (192.168.1.101) |
| 8080 | API Server | Master (192.168.1.100) |

### File Locations

| Path | Purpose |
|------|---------|
| `C:\llama.cpp\bin\` | Executables |
| `C:\llama.cpp\models\` | GGUF models |
| `C:\llama.cpp\cache\` | RPC cache |
| `C:\llama.cpp\logs\` | Log files |

### Environment Variables

| Variable | Value |
|----------|-------|
| `CUDA_PATH` | `C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v12.4` |
| `LLAMA_CACHE` | `C:\llama.cpp\cache` |

---

## Appendix B: Model Size Reference

| Model | Quantization | File Size | VRAM Required | Recommended For |
|-------|--------------|-----------|---------------|-----------------|
| Qwen2.5-14B | Q4_K_M | ~9 GB | ~12 GB | Balanced performance |
| Llama-3.1-70B | Q4_K_M | ~38 GB | ~42 GB | **Best overall** |
| Mixtral-8x22B | Q4_K_M | ~67 GB | ~72 GB | Maximum capability |
| Llama-3.1-8B | Q8_0 | ~8 GB | ~10 GB | Fast inference |

---

*End of Phase 1 Infrastructure & RPC Cluster Setup Guide*
