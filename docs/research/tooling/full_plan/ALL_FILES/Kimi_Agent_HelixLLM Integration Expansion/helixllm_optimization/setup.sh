#!/bin/bash
###############################################################################
# HelixLLM Master Setup Script
# Complete installation and configuration for optimal performance
###############################################################################

set -e  # Exit on error

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "==============================================="
echo "         HelixLLM Master Setup"
echo "==============================================="
echo ""

# Check if running on supported OS
if [[ "$OSTYPE" != "linux-gnu"* ]]; then
    echo -e "${RED}Error: This script is designed for Linux${NC}"
    echo "For Windows, use setup.bat instead"
    exit 1
fi

# Function to print section headers
print_section() {
    echo ""
    echo -e "${CYAN}===============================================${NC}"
    echo -e "${CYAN}$1${NC}"
    echo -e "${CYAN}===============================================${NC}"
    echo ""
}

# Function to check command success
check_result() {
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ $1${NC}"
    else
        echo -e "${RED}✗ $1 failed${NC}"
        if [ "$2" = "required" ]; then
            exit 1
        fi
    fi
}

# ============================================================================
# STEP 1: Environment Setup
# ============================================================================
print_section "Step 1: Environment Setup"

if [ -f "01_environment_setup.sh" ]; then
    chmod +x 01_environment_setup.sh
    ./01_environment_setup.sh
    check_result "Environment setup" "required"
else
    echo -e "${RED}Error: 01_environment_setup.sh not found${NC}"
    exit 1
fi

# Source the environment
if [ -f "$HOME/.config/helixllm/environment.sh" ]; then
    source "$HOME/.config/helixllm/environment.sh"
fi

# ============================================================================
# STEP 2: Build llama.cpp
# ============================================================================
print_section "Step 2: Building llama.cpp"

if [ -f "02_build_llama_cpp.sh" ]; then
    chmod +x 02_build_llama_cpp.sh
    ./02_build_llama_cpp.sh
    check_result "llama.cpp build" "required"
else
    echo -e "${RED}Error: 02_build_llama_cpp.sh not found${NC}"
    exit 1
fi

# ============================================================================
# STEP 3: Install llama-cpp-python
# ============================================================================
print_section "Step 3: Installing llama-cpp-python"

if [ -f "03_install_llama_cpp_python.sh" ]; then
    chmod +x 03_install_llama_cpp_python.sh
    ./03_install_llama_cpp_python.sh
    check_result "llama-cpp-python installation" "required"
else
    echo -e "${RED}Error: 03_install_llama_cpp_python.sh not found${NC}"
    exit 1
fi

# ============================================================================
# STEP 4: Verify Installation
# ============================================================================
print_section "Step 4: Verifying Installation"

# Activate virtual environment
VENV_PATH="${HOME}/helixllm_env"
if [ -f "$VENV_PATH/bin/activate" ]; then
    source "$VENV_PATH/bin/activate"
    echo -e "${GREEN}✓ Virtual environment activated${NC}"
fi

# Test Python imports
python3 << 'PYTHON_EOF'
import sys
print(f"Python: {sys.executable}")
print(f"Version: {sys.version}")

try:
    import llama_cpp
    print(f"✓ llama-cpp-python: {llama_cpp.__version__}")
except ImportError as e:
    print(f"✗ llama-cpp-python not found: {e}")
    sys.exit(1)

try:
    import psutil
    print(f"✓ psutil: {psutil.__version__}")
except ImportError:
    print("⚠ psutil not found (optional)")

try:
    import pynvml
    print("✓ pynvml: available")
except ImportError:
    print("⚠ pynvml not found (optional)")

print("\n✓ All required packages installed")
PYTHON_EOF

check_result "Package verification"

# ============================================================================
# STEP 5: Run Hardware Detection
# ============================================================================
print_section "Step 5: Hardware Detection"

if [ -f "06_hardware_detection.py" ]; then
    python3 06_hardware_detection.py
    check_result "Hardware detection"
else
    echo -e "${YELLOW}Warning: 06_hardware_detection.py not found${NC}"
fi

# ============================================================================
# STEP 6: Download Models
# ============================================================================
print_section "Step 6: Downloading Models"

echo "Models will be downloaded to: ${SCRIPT_DIR}/models"
echo ""
echo "Recommended models for 6GB VRAM:"
echo "  - Qwen2.5-1.5B-Instruct-Q4_K_M.gguf (~1GB)"
echo "  - nomic-embed-text-v1.5.Q4_K_M.gguf (~300MB)"
echo ""
echo -n "Download recommended models now? (y/n): "
read -r response

if [[ "$response" =~ ^[Yy]$ ]]; then
    if [ -f "11_download_models.py" ]; then
        python3 11_download_models.py --download-all
        check_result "Model download"
    else
        echo -e "${YELLOW}Warning: 11_download_models.py not found${NC}"
        echo "Please download models manually from HuggingFace"
    fi
else
    echo "Skipping model download. Run 'python 11_download_models.py' later."
fi

# ============================================================================
# STEP 7: Run Optimization Checklist
# ============================================================================
print_section "Step 7: Optimization Checklist"

if [ -f "08_optimization_checklist.py" ]; then
    python3 08_optimization_checklist.py
    check_result "Optimization checklist"
else
    echo -e "${YELLOW}Warning: 08_optimization_checklist.py not found${NC}"
fi

# ============================================================================
# STEP 8: Run Benchmark
# ============================================================================
print_section "Step 8: Running Benchmark"

echo "This will test the performance of your setup."
echo -n "Run benchmark now? (y/n): "
read -r response

if [[ "$response" =~ ^[Yy]$ ]]; then
    if [ -f "09_benchmark.py" ]; then
        python3 09_benchmark.py
        check_result "Benchmark"
    else
        echo -e "${YELLOW}Warning: 09_benchmark.py not found${NC}"
    fi
else
    echo "Skipping benchmark. Run 'python 09_benchmark.py' later."
fi

# ============================================================================
# Setup Complete
# ============================================================================
print_section "Setup Complete!"

echo -e "${GREEN}HelixLLM has been successfully set up!${NC}"
echo ""
echo "Next steps:"
echo ""
echo "1. Activate the virtual environment:"
echo "   source ${HOME}/helixllm_env/bin/activate"
echo ""
echo "2. Source the environment configuration:"
echo "   source ${HOME}/.config/helixllm/environment.sh"
echo ""
echo "3. Start the HelixLLM server:"
echo "   python 10_helixllm_server.py"
echo ""
echo "4. Or run a quick test:"
echo "   python 04_model_loader.py"
echo ""
echo "Configuration files:"
echo "  - Environment: ${HOME}/.config/helixllm/environment.sh"
echo "  - Profiles: ${HOME}/.config/helixllm/profiles/"
echo "  - Benchmarks: ${HOME}/.config/helixllm/benchmarks/"
echo ""
echo "Useful commands:"
echo "  - List models: python 11_download_models.py --list"
echo "  - Run benchmark: python 09_benchmark.py"
echo "  - Check optimizations: python 08_optimization_checklist.py"
echo ""
echo -e "${GREEN}Happy inferencing!${NC}"
echo ""
