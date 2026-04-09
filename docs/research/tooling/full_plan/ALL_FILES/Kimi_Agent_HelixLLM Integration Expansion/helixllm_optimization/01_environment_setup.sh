#!/bin/bash
###############################################################################
# HelixLLM Environment Setup Script
# Optimized for: AMD Ryzen 9, 32GB RAM, RTX GPU (6GB VRAM), 2TB NVMe SSD
# Target: 150-300+ tokens/sec, 10-20 docs/sec embeddings, <50ms retrieval
###############################################################################

set -e  # Exit on error

echo "==============================================="
echo "HelixLLM Environment Setup"
echo "==============================================="

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Detect OS
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    OS="linux"
    if command -v apt-get &> /dev/null; then
        DISTRO="ubuntu"
    elif command -v yum &> /dev/null; then
        DISTRO="rhel"
    elif command -v pacman &> /dev/null; then
        DISTRO="arch"
    fi
elif [[ "$OSTYPE" == "msys" ]] || [[ "$OSTYPE" == "cygwin" ]] || [[ "$OSTYPE" == "win32" ]]; then
    OS="windows"
else
    echo -e "${RED}Unsupported OS: $OSTYPE${NC}"
    exit 1
fi

echo -e "${BLUE}Detected OS: $OS${NC}"

###############################################################################
# STEP 1: NVIDIA Driver Installation/Verification
###############################################################################
echo ""
echo -e "${GREEN}Step 1: NVIDIA Driver Verification${NC}"
echo "-----------------------------------------------"

if command -v nvidia-smi &> /dev/null; then
    echo -e "${GREEN}NVIDIA drivers already installed${NC}"
    nvidia-smi
    
    # Extract driver version
    DRIVER_VERSION=$(nvidia-smi --query-gpu=driver_version --format=csv,noheader | head -n1)
    echo -e "${BLUE}Driver Version: $DRIVER_VERSION${NC}"
    
    # Check if driver is recent enough (>= 525.60.13 for CUDA 12.0)
    REQUIRED_DRIVER="525.60.13"
    if [ "$(printf '%s\n' "$REQUIRED_DRIVER" "$DRIVER_VERSION" | sort -V | head -n1)" = "$REQUIRED_DRIVER" ]; then
        echo -e "${GREEN}Driver version is sufficient${NC}"
    else
        echo -e "${YELLOW}Warning: Driver version may be outdated. Recommended: >= $REQUIRED_DRIVER${NC}"
    fi
else
    echo -e "${YELLOW}NVIDIA drivers not found. Installing...${NC}"
    
    if [ "$DISTRO" == "ubuntu" ]; then
        # Ubuntu driver installation
        sudo apt-get update
        sudo apt-get install -y ubuntu-drivers-common
        sudo ubuntu-drivers autoinstall
        
        # Alternative: Install specific driver version
        # sudo apt-get install -y nvidia-driver-535
        
    elif [ "$DISTRO" == "rhel" ]; then
        # RHEL/CentOS driver installation
        sudo yum install -y epel-release
        sudo yum install -y kernel-devel kernel-headers
        sudo yum install -y nvidia-driver-latest-dkms
        
    elif [ "$OS" == "windows" ]; then
        echo -e "${RED}Please install NVIDIA drivers manually from https://www.nvidia.com/drivers${NC}"
        exit 1
    fi
    
    echo -e "${YELLOW}Please reboot and run this script again${NC}"
    exit 0
fi

###############################################################################
# STEP 2: CUDA Toolkit Installation
###############################################################################
echo ""
echo -e "${GREEN}Step 2: CUDA Toolkit Installation${NC}"
echo "-----------------------------------------------"

CUDA_VERSION="12.1"  # Optimal for RTX GPUs - good balance of features and compatibility
CUDA_MAJOR="12"
CUDA_MINOR="1"

if command -v nvcc &> /dev/null; then
    INSTALLED_CUDA=$(nvcc --version | grep "release" | sed -n 's/.*release \([0-9]\+\.[0-9]\+\).*/\1/p')
    echo -e "${GREEN}CUDA already installed: $INSTALLED_CUDA${NC}"
    
    # Check if version is compatible
    if [[ "$INSTALLED_CUDA" == "$CUDA_MAJOR"* ]]; then
        echo -e "${GREEN}CUDA version is compatible${NC}"
    else
        echo -e "${YELLOW}Warning: CUDA $INSTALLED_CUDA installed, but $CUDA_VERSION recommended${NC}"
    fi
else
    echo -e "${YELLOW}Installing CUDA Toolkit $CUDA_VERSION...${NC}"
    
    if [ "$DISTRO" == "ubuntu" ]; then
        # Ubuntu CUDA installation
        wget https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/x86_64/cuda-ubuntu2204.pin
        sudo mv cuda-ubuntu2204.pin /etc/apt/preferences.d/cuda-repository-pin-600
        
        # Download and install CUDA
        CUDA_DEB="cuda-repo-ubuntu2204-${CUDA_MAJOR}-${CUDA_MINOR}-local_${CUDA_VERSION}.0-530.30.02-1_amd64.deb"
        wget https://developer.download.nvidia.com/compute/cuda/${CUDA_VERSION}.0/local_installers/${CUDA_DEB}
        sudo dpkg -i ${CUDA_DEB}
        sudo cp /var/cuda-repo-ubuntu2204-${CUDA_MAJOR}-${CUDA_MINOR}-local/cuda-*-keyring.gpg /usr/share/keyrings/
        sudo apt-get update
        sudo apt-get -y install cuda-toolkit-${CUDA_MAJOR}-${CUDA_MINOR}
        
        # Cleanup
        rm -f ${CUDA_DEB}
        
    elif [ "$DISTRO" == "rhel" ]; then
        # RHEL CUDA installation
        sudo yum install -y cuda-toolkit-${CUDA_MAJOR}-${CUDA_MINOR}
        
    elif [ "$OS" == "windows" ]; then
        echo -e "${RED}Please install CUDA Toolkit manually from https://developer.nvidia.com/cuda-downloads${NC}"
        exit 1
    fi
fi

# Set CUDA environment variables
echo ""
echo -e "${BLUE}Setting CUDA environment variables...${NC}"

CUDA_HOME="/usr/local/cuda-${CUDA_VERSION}"
if [ ! -d "$CUDA_HOME" ]; then
    CUDA_HOME="/usr/local/cuda"
fi

# Add to shell profile
SHELL_PROFILE=""
if [ -f "$HOME/.bashrc" ]; then
    SHELL_PROFILE="$HOME/.bashrc"
elif [ -f "$HOME/.zshrc" ]; then
    SHELL_PROFILE="$HOME/.zshrc"
fi

if [ -n "$SHELL_PROFILE" ]; then
    # Check if already added
    if ! grep -q "CUDA_HOME" "$SHELL_PROFILE"; then
        echo "" >> "$SHELL_PROFILE"
        echo "# CUDA Environment Variables" >> "$SHELL_PROFILE"
        echo "export CUDA_HOME=$CUDA_HOME" >> "$SHELL_PROFILE"
        echo 'export PATH=$CUDA_HOME/bin:$PATH' >> "$SHELL_PROFILE"
        echo 'export LD_LIBRARY_PATH=$CUDA_HOME/lib64:$LD_LIBRARY_PATH' >> "$SHELL_PROFILE"
        echo -e "${GREEN}CUDA environment variables added to $SHELL_PROFILE${NC}"
    else
        echo -e "${YELLOW}CUDA environment variables already in $SHELL_PROFILE${NC}"
    fi
fi

# Export for current session
export CUDA_HOME=$CUDA_HOME
export PATH=$CUDA_HOME/bin:$PATH
export LD_LIBRARY_PATH=$CUDA_HOME/lib64:$LD_LIBRARY_PATH

echo -e "${GREEN}CUDA_HOME: $CUDA_HOME${NC}"

###############################################################################
# STEP 3: cuDNN Installation
###############################################################################
echo ""
echo -e "${GREEN}Step 3: cuDNN Installation${NC}"
echo "-----------------------------------------------"

CUDNN_VERSION="8.9.7"

if [ -f "/usr/local/cuda/include/cudnn.h" ] || [ -f "/usr/include/cudnn.h" ]; then
    echo -e "${GREEN}cuDNN already installed${NC}"
else
    echo -e "${YELLOW}Installing cuDNN ${CUDNN_VERSION}...${NC}"
    
    if [ "$DISTRO" == "ubuntu" ]; then
        # Download cuDNN (requires NVIDIA account - manual step)
        echo -e "${YELLOW}Note: cuDNN requires manual download from NVIDIA${NC}"
        echo -e "${YELLOW}Please download from: https://developer.nvidia.com/cudnn${NC}"
        echo -e "${YELLOW}Then run: sudo dpkg -i libcudnn8_*.deb libcudnn8-dev_*.deb${NC}"
        
        # Alternative: Install from repository (if available)
        # sudo apt-get install -y libcudnn8 libcudnn8-dev
        
    elif [ "$DISTRO" == "rhel" ]; then
        sudo yum install -y libcudnn8 libcudnn8-devel
    fi
fi

###############################################################################
# STEP 4: System Package Installation
###############################################################################
echo ""
echo -e "${GREEN}Step 4: System Package Installation${NC}"
echo "-----------------------------------------------"

if [ "$DISTRO" == "ubuntu" ]; then
    sudo apt-get update
    sudo apt-get install -y \
        build-essential \
        cmake \
        git \
        wget \
        curl \
        python3 \
        python3-pip \
        python3-venv \
        python3-dev \
        libopenblas-dev \
        libomp-dev \
        ninja-build \
        pkg-config \
        libssl-dev
        
    # Additional packages for optimal performance
    sudo apt-get install -y \
        numactl \
        linux-tools-common \
        linux-tools-generic \
        cpufrequtils
        
elif [ "$DISTRO" == "rhel" ]; then
    sudo yum groupinstall -y "Development Tools"
    sudo yum install -y \
        cmake \
        git \
        wget \
        curl \
        python3 \
        python3-pip \
        python3-devel \
        openblas-devel \
        libgomp \
        ninja-build \
        openssl-devel
        
elif [ "$OS" == "windows" ]; then
    echo -e "${YELLOW}Please ensure the following are installed:${NC}"
    echo "  - Visual Studio 2019/2022 with C++ build tools"
    echo "  - CMake"
    echo "  - Git for Windows"
    echo "  - Python 3.10+"
fi

echo -e "${GREEN}System packages installed${NC}"

###############################################################################
# STEP 5: Python Environment Setup
###############################################################################
echo ""
echo -e "${GREEN}Step 5: Python Environment Setup${NC}"
echo "-----------------------------------------------"

PYTHON_VERSION="3.11"  # Optimal balance of features and compatibility

# Check Python version
if command -v python3 &> /dev/null; then
    CURRENT_PY=$(python3 --version 2>&1 | awk '{print $2}')
    echo -e "${BLUE}Python version: $CURRENT_PY${NC}"
else
    echo -e "${RED}Python3 not found. Installing...${NC}"
    
    if [ "$DISTRO" == "ubuntu" ]; then
        sudo apt-get install -y python${PYTHON_VERSION} python${PYTHON_VERSION}-venv python${PYTHON_VERSION}-dev
    elif [ "$DISTRO" == "rhel" ]; then
        sudo yum install -y python${PYTHON_VERSION} python${PYTHON_VERSION}-devel
    fi
fi

# Create virtual environment
VENV_PATH="${HOME}/helixllm_env"
echo -e "${BLUE}Creating virtual environment at $VENV_PATH${NC}"

if [ -d "$VENV_PATH" ]; then
    echo -e "${YELLOW}Virtual environment already exists${NC}"
else
    python3 -m venv "$VENV_PATH"
    echo -e "${GREEN}Virtual environment created${NC}"
fi

# Activate and upgrade pip
echo -e "${BLUE}Upgrading pip...${NC}"
source "$VENV_PATH/bin/activate"
pip install --upgrade pip setuptools wheel

echo -e "${GREEN}Python environment ready${NC}"

###############################################################################
# STEP 6: CPU Optimization (for AMD Ryzen)
###############################################################################
echo ""
echo -e "${GREEN}Step 6: CPU Optimization${NC}"
echo "-----------------------------------------------"

# Detect CPU
CPU_INFO=$(cat /proc/cpuinfo | grep "model name" | head -n1 | cut -d: -f2 | xargs)
echo -e "${BLUE}CPU: $CPU_INFO${NC}"

# Set CPU governor to performance
if command -v cpufreq-set &> /dev/null; then
    echo -e "${BLUE}Setting CPU governor to performance mode...${NC}"
    for cpu in /sys/devices/system/cpu/cpu[0-9]*; do
        echo performance | sudo tee ${cpu}/cpufreq/scaling_governor > /dev/null 2>&1 || true
    done
    echo -e "${GREEN}CPU governor set to performance${NC}"
fi

# Disable CPU frequency scaling (if possible)
if [ -f "/sys/devices/system/cpu/intel_pstate/no_turbo" ]; then
    echo 1 | sudo tee /sys/devices/system/cpu/intel_pstate/no_turbo > /dev/null 2>&1 || true
fi

# Set process priority (for current session)
if command -v renice &> /dev/null; then
    renice -n -10 $$ > /dev/null 2>&1 || true
fi

echo -e "${GREEN}CPU optimizations applied${NC}"

###############################################################################
# STEP 7: Memory Optimization
###############################################################################
echo ""
echo -e "${GREEN}Step 7: Memory Optimization${NC}"
echo "-----------------------------------------------"

# Increase vm.max_map_count for large models
echo -e "${BLUE}Setting vm.max_map_count...${NC}"
echo 'vm.max_map_count=262144' | sudo tee -a /etc/sysctl.conf > /dev/null 2>&1 || true
sudo sysctl -w vm.max_map_count=262144 > /dev/null 2>&1 || true

# Increase file descriptor limits
echo -e "${BLUE}Setting file descriptor limits...${NC}"
echo '* soft nofile 65536' | sudo tee -a /etc/security/limits.conf > /dev/null 2>&1 || true
echo '* hard nofile 65536' | sudo tee -a /etc/security/limits.conf > /dev/null 2>&1 || true

# Disable swap for better performance (optional - use with caution)
# echo -e "${YELLOW}Disabling swap...${NC}"
# sudo swapoff -a

echo -e "${GREEN}Memory optimizations applied${NC}"

###############################################################################
# STEP 8: Create Environment Configuration File
###############################################################################
echo ""
echo -e "${GREEN}Step 8: Creating Environment Configuration${NC}"
echo "-----------------------------------------------"

CONFIG_DIR="${HOME}/.config/helixllm"
mkdir -p "$CONFIG_DIR"

cat > "${CONFIG_DIR}/environment.sh" << 'EOF'
#!/bin/bash
# HelixLLM Environment Configuration
# Source this file: source ~/.config/helixllm/environment.sh

# CUDA Configuration
export CUDA_HOME=/usr/local/cuda
export PATH=$CUDA_HOME/bin:$PATH
export LD_LIBRARY_PATH=$CUDA_HOME/lib64:$LD_LIBRARY_PATH

# llama.cpp Performance Variables
export LLAMA_CUDA_FORCE_MMQ=1           # Force MMQ for better performance
export LLAMA_CUDA_MMV_Y=1               # MMV optimization
export LLAMA_CUDA_F16=1                 # Use FP16 for GPU operations
export LLAMA_CUDA_DMMV_X=32             # DMMV X dimension
export LLAMA_CUDA_DMMV_Y=1              # DMMV Y dimension
export LLAMA_CUDA_KQUANTS_ITER=2        # K-quants iterations
export LLAMA_CUDA_PEER_MAX_BATCH_SIZE=128  # Peer access batch size

# CPU Threading
export OMP_NUM_THREADS=16               # Adjust based on your CPU cores
export OPENBLAS_NUM_THREADS=16
export MKL_NUM_THREADS=16

# Memory Management
export GGML_CUDA_NO_PINNED=0            # Allow pinned memory
export GGML_CUDA_MEMORY_POOL=1          # Enable memory pooling

# Python Optimization
export PYTHONUNBUFFERED=1
export PYTHONDONTWRITEBYTECODE=1

# PyTorch (if used)
export PYTORCH_CUDA_ALLOC_CONF=max_split_size_mb:512

# HuggingFace Cache
export HF_HOME="${HOME}/.cache/huggingface"
export TRANSFORMERS_CACHE="${HF_HOME}/transformers"

# llama.cpp Cache
export LLAMA_CACHE="${HOME}/.cache/llama.cpp"
mkdir -p "$LLAMA_CACHE"

echo "HelixLLM environment loaded"
EOF

chmod +x "${CONFIG_DIR}/environment.sh"

echo -e "${GREEN}Environment configuration created at ${CONFIG_DIR}/environment.sh${NC}"
echo -e "${YELLOW}Add to your shell profile: source ${CONFIG_DIR}/environment.sh${NC}"

###############################################################################
# STEP 9: Verification
###############################################################################
echo ""
echo -e "${GREEN}Step 9: Verification${NC}"
echo "-----------------------------------------------"

echo -e "${BLUE}Checking NVIDIA GPU...${NC}"
nvidia-smi --query-gpu=name,memory.total,memory.free,compute_cap --format=csv

echo ""
echo -e "${BLUE}Checking CUDA...${NC}"
nvcc --version

echo ""
echo -e "${BLUE}Checking Python...${NC}"
which python3
python3 --version

echo ""
echo -e "${BLUE}Virtual Environment: $VENV_PATH${NC}"

echo ""
echo -e "${GREEN}===============================================${NC}"
echo -e "${GREEN}Environment Setup Complete!${NC}"
echo -e "${GREEN}===============================================${NC}"
echo ""
echo "Next steps:"
echo "  1. Activate virtual environment: source $VENV_PATH/bin/activate"
echo "  2. Run build script: ./02_build_llama_cpp.sh"
echo "  3. Source environment: source ${CONFIG_DIR}/environment.sh"
echo ""
