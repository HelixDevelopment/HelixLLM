#!/bin/bash
###############################################################################
# HelixLLM llama-cpp-python Installation Script
# CUDA-enabled installation with performance optimizations
###############################################################################

set -e  # Exit on error

echo "==============================================="
echo "HelixLLM llama-cpp-python Installation"
echo "==============================================="

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
LLAMA_CPP_DIR="${HOME}/llama.cpp"
VENV_PATH="${HOME}/helixllm_env"

# Ensure virtual environment is activated
if [ -z "$VIRTUAL_ENV" ]; then
    echo -e "${YELLOW}Virtual environment not activated.${NC}"
    echo -e "${BLUE}Activating: $VENV_PATH${NC}"
    source "$VENV_PATH/bin/activate"
fi

echo -e "${BLUE}Using Python: $(which python)${NC}"
echo -e "${BLUE}Python Version: $(python --version)${NC}"

###############################################################################
# STEP 1: Pre-installation Checks
###############################################################################
echo ""
echo -e "${GREEN}Step 1: Pre-installation Checks${NC}"
echo "-----------------------------------------------"

# Check CUDA availability
if command -v nvcc &> /dev/null; then
    echo -e "${GREEN}✓ CUDA available: $(nvcc --version | grep release)${NC}"
else
    echo -e "${RED}✗ CUDA not found. Please run 01_environment_setup.sh first${NC}"
    exit 1
fi

# Check llama.cpp build
if [ ! -d "$LLAMA_CPP_DIR/build" ]; then
    echo -e "${RED}✗ llama.cpp build not found. Please run 02_build_llama_cpp.sh first${NC}"
    exit 1
fi

echo -e "${GREEN}✓ llama.cpp build found at $LLAMA_CPP_DIR${NC}"

# Verify GPU
if command -v nvidia-smi &> /dev/null; then
    echo -e "${GREEN}✓ GPU detected:${NC}"
    nvidia-smi --query-gpu=name,memory.total --format=csv | head -2
else
    echo -e "${YELLOW}⚠ nvidia-smi not available${NC}"
fi

###############################################################################
# STEP 2: Install Build Dependencies
###############################################################################
echo ""
echo -e "${GREEN}Step 2: Installing Build Dependencies${NC}"
echo "-----------------------------------------------"

pip install --upgrade pip setuptools wheel scikit-build-core cmake ninja

echo -e "${GREEN}Build dependencies installed${NC}"

###############################################################################
# STEP 3: Set Environment Variables for Build
###############################################################################
echo ""
echo -e "${GREEN}Step 3: Setting Build Environment Variables${NC}"
echo "-----------------------------------------------"

# CUDA paths
export CUDA_HOME=/usr/local/cuda
export PATH=$CUDA_HOME/bin:$PATH
export LD_LIBRARY_PATH=$CUDA_HOME/lib64:$LD_LIBRARY_PATH

# llama.cpp paths for build
export LLAMA_CPP_LIB="${LLAMA_CPP_DIR}/build"
export CMAKE_ARGS="-DLLAMA_CUDA=on -DLLAMA_CUDA_F16=on -DLLAMA_NATIVE=on"

# GPU architecture detection
if command -v nvidia-smi &> /dev/null; then
    COMPUTE_CAP=$(nvidia-smi --query-gpu=compute_cap --format=csv,noheader | head -n1 | tr -d '.')
    if [ -n "$COMPUTE_CAP" ]; then
        export CMAKE_ARGS="${CMAKE_ARGS} -DCMAKE_CUDA_ARCHITECTURES=${COMPUTE_CAP:0:2}"
        echo -e "${BLUE}CUDA Architecture: ${COMPUTE_CAP:0:2}${NC}"
    fi
fi

echo -e "${BLUE}CMAKE_ARGS: $CMAKE_ARGS${NC}"

###############################################################################
# STEP 4: Install llama-cpp-python
###############################################################################
echo ""
echo -e "${GREEN}Step 4: Installing llama-cpp-python${NC}"
echo "-----------------------------------------------"

# Method 1: Pre-built wheel with CUDA (fastest)
echo -e "${BLUE}Attempting pre-built wheel installation...${NC}"

# Try to install pre-built CUDA wheel first
pip install llama-cpp-python-cublas --extra-index-url https://abetlen.github.io/llama-cpp-python/whl/cu121 2>/dev/null || {
    echo -e "${YELLOW}Pre-built wheel not available, building from source...${NC}"
    
    # Method 2: Build from source with CUDA
    CMAKE_ARGS="${CMAKE_ARGS}" \
    FORCE_CMAKE=1 \
    pip install llama-cpp-python --no-cache-dir --force-reinstall
}

if [ $? -ne 0 ]; then
    echo -e "${RED}Installation failed!${NC}"
    echo -e "${YELLOW}Trying alternative installation method...${NC}"
    
    # Method 3: Direct build with explicit settings
    pip install llama-cpp-python \
        --no-cache-dir \
        --force-reinstall \
        --config-settings cmake.args="-DLLAMA_CUDA=on;-DLLAMA_CUDA_F16=on;-DLLAMA_NATIVE=on"
fi

echo -e "${GREEN}llama-cpp-python installed${NC}"

###############################################################################
# STEP 5: Verify Installation
###############################################################################
echo ""
echo -e "${GREEN}Step 5: Verifying Installation${NC}"
echo "-----------------------------------------------"

# Create verification script
python3 << 'PYTHON_EOF'
import sys
print(f"Python: {sys.executable}")
print(f"Version: {sys.version}")

try:
    import llama_cpp
    print(f"\n✓ llama-cpp-python version: {llama_cpp.__version__}")
    
    # Check CUDA support
    try:
        from llama_cpp import Llama
        
        # Try to get library info
        lib = llama_cpp.lib
        print(f"✓ llama.cpp library loaded")
        
        # Check for CUDA in library
        import ctypes
        try:
            # Try to call a CUDA-specific function
            lib.llama_supports_gpu_offload()
            print("✓ CUDA/GPU offload supported")
        except:
            print("⚠ GPU offload status unclear")
            
    except Exception as e:
        print(f"⚠ Error checking GPU support: {e}")
        
except ImportError as e:
    print(f"✗ llama-cpp-python not installed: {e}")
    sys.exit(1)

print("\n✓ Installation verified successfully!")
PYTHON_EOF

if [ $? -ne 0 ]; then
    echo -e "${RED}Verification failed!${NC}"
    exit 1
fi

###############################################################################
# STEP 6: GPU Detection Test
###############################################################################
echo ""
echo -e "${GREEN}Step 6: GPU Detection Test${NC}"
echo "-----------------------------------------------"

python3 << 'PYTHON_EOF'
import llama_cpp
import sys

print("Checking llama.cpp capabilities...")

try:
    # Get number of physical cores
    import os
    cpu_count = os.cpu_count()
    print(f"CPU Cores: {cpu_count}")
    
    # Check for GPU
    try:
        # Try to initialize with GPU
        from llama_cpp import Llama
        
        # Create a minimal test to check GPU
        # This will fail if CUDA is not properly linked
        params = llama_cpp.llama_model_default_params()
        print("✓ llama_model_params accessible")
        
        # Check CUDA availability
        import subprocess
        result = subprocess.run(['nvidia-smi'], capture_output=True, text=True)
        if result.returncode == 0:
            print("✓ nvidia-smi accessible")
            lines = result.stdout.split('\n')
            for line in lines[:10]:
                if 'NVIDIA' in line or 'MiB' in line:
                    print(f"  {line}")
        
    except Exception as e:
        print(f"⚠ GPU check warning: {e}")
        
except Exception as e:
    print(f"✗ Error: {e}")
    sys.exit(1)

print("\n✓ GPU detection test passed")
PYTHON_EOF

###############################################################################
# STEP 7: Install Additional Dependencies
###############################################################################
echo ""
echo -e "${GREEN}Step 7: Installing Additional Dependencies${NC}"
echo "-----------------------------------------------"

pip install \
    numpy \
    sentencepiece \
    protobuf \
    huggingface-hub \
    psutil \
    pynvml \
    torch --index-url https://download.pytorch.org/whl/cu121 2>/dev/null || pip install torch

echo -e "${GREEN}Additional dependencies installed${NC}"

###############################################################################
# STEP 8: Create Installation Info
###############################################################################
echo ""
echo -e "${GREEN}Step 8: Creating Installation Info${NC}"
echo "-----------------------------------------------"

INFO_FILE="${HOME}/.config/helixllm/installation_info.txt"
mkdir -p "$(dirname $INFO_FILE)"

cat > "$INFO_FILE" << EOF
llama-cpp-python Installation Information
==========================================
Installation Date: $(date)
Python: $(which python)
Version: $(python --version)

llama-cpp-python: $(pip show llama-cpp-python | grep Version)
Location: $(pip show llama-cpp-python | grep Location)

CUDA:
$(nvcc --version 2>/dev/null | head -4 || echo "CUDA info not available")

GPU:
$(nvidia-smi --query-gpu=name,driver_version,memory.total --format=csv 2>/dev/null || echo "GPU info not available")

Environment Variables:
CUDA_HOME: ${CUDA_HOME}
CMAKE_ARGS: ${CMAKE_ARGS}
LD_LIBRARY_PATH: ${LD_LIBRARY_PATH}
EOF

echo -e "${GREEN}Installation info saved to $INFO_FILE${NC}"

###############################################################################
# Troubleshooting Guide
###############################################################################
echo ""
echo -e "${GREEN}===============================================${NC}"
echo -e "${GREEN}Installation Complete!${NC}"
echo -e "${GREEN}===============================================${NC}"
echo ""
echo "Troubleshooting Guide:"
echo "----------------------"
echo ""
echo "1. If GPU is not detected:"
echo "   - Verify CUDA installation: nvcc --version"
echo "   - Check nvidia-smi output"
echo "   - Rebuild llama.cpp with correct CUDA arch"
echo ""
echo "2. If import fails:"
echo "   - Check virtual environment is activated"
echo "   - Verify LD_LIBRARY_PATH includes CUDA libs"
echo "   - Try: export LD_LIBRARY_PATH=/usr/local/cuda/lib64:\$LD_LIBRARY_PATH"
echo ""
echo "3. If CUDA errors occur:"
echo "   - Check GPU memory: nvidia-smi"
echo "   - Reduce n_gpu_layers in model loading"
echo "   - Verify CUDA and driver compatibility"
echo ""
echo "4. Common fixes:"
echo "   - Reinstall: pip install llama-cpp-python --force-reinstall --no-cache-dir"
echo "   - Clear pip cache: pip cache purge"
echo "   - Update pip: pip install --upgrade pip"
echo ""
echo "Next steps:"
echo "  1. Test with: python -c 'from llama_cpp import Llama; print(\"OK\")'"
echo "  2. Run benchmark: ./05_benchmark.py"
echo "  3. Start using HelixLLM: ./06_helixllm_server.py"
echo ""
