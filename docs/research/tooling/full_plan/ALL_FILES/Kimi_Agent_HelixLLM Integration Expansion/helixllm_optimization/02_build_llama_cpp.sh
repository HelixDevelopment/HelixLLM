#!/bin/bash
###############################################################################
# HelixLLM llama.cpp Build Script
# Optimized for: AMD Ryzen 9, 32GB RAM, RTX GPU (6GB VRAM)
# Build flags optimized for maximum performance
###############################################################################

set -e  # Exit on error

echo "==============================================="
echo "HelixLLM llama.cpp Build Configuration"
echo "==============================================="

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
LLAMA_CPP_DIR="${HOME}/llama.cpp"
BUILD_TYPE="Release"
CUDA_ARCHITECTURES="75;80;86;89"  # Turing, Ampere, Ada Lovelace (RTX 20/30/40 series)

# Detect GPU compute capability
detect_gpu_arch() {
    if command -v nvidia-smi &> /dev/null; then
        # Get compute capability from nvidia-smi
        COMPUTE_CAP=$(nvidia-smi --query-gpu=compute_cap --format=csv,noheader | head -n1 | tr -d '.')
        if [ -n "$COMPUTE_CAP" ]; then
            echo -e "${GREEN}Detected GPU Compute Capability: $COMPUTE_CAP${NC}"
            
            # Map to architecture
            case "${COMPUTE_CAP:0:2}" in
                "75") ARCH="Turing (RTX 20 series)" ;;
                "80") ARCH="Ampere (RTX 30 series)" ;;
                "86") ARCH="Ampere (RTX 30 series)" ;;
                "89") ARCH="Ada Lovelace (RTX 40 series)" ;;
                "90") ARCH="Hopper" ;;
                *) ARCH="Unknown" ;;
            esac
            echo -e "${BLUE}GPU Architecture: $ARCH${NC}"
            
            # Set specific architecture for build
            CUDA_ARCHITECTURES="${COMPUTE_CAP:0:2}"
        fi
    fi
}

###############################################################################
# STEP 1: Clone/Update llama.cpp Repository
###############################################################################
echo ""
echo -e "${GREEN}Step 1: Repository Setup${NC}"
echo "-----------------------------------------------"

if [ -d "$LLAMA_CPP_DIR" ]; then
    echo -e "${YELLOW}llama.cpp directory exists. Updating...${NC}"
    cd "$LLAMA_CPP_DIR"
    git pull origin master
    git submodule update --init --recursive
else
    echo -e "${BLUE}Cloning llama.cpp repository...${NC}"
    git clone --recursive https://github.com/ggerganov/llama.cpp.git "$LLAMA_CPP_DIR"
    cd "$LLAMA_CPP_DIR"
fi

# Checkout a stable commit (optional - for reproducibility)
# git checkout bXXXX  # Uncomment and specify commit hash for stability

echo -e "${GREEN}Repository ready at $LLAMA_CPP_DIR${NC}"

###############################################################################
# STEP 2: Detect GPU Architecture
###############################################################################
echo ""
echo -e "${GREEN}Step 2: GPU Detection${NC}"
echo "-----------------------------------------------"

detect_gpu_arch

###############################################################################
# STEP 3: Clean Previous Builds
###############################################################################
echo ""
echo -e "${GREEN}Step 3: Cleaning Previous Builds${NC}"
echo "-----------------------------------------------"

rm -rf build
mkdir -p build
cd build

echo -e "${GREEN}Build directory cleaned${NC}"

###############################################################################
# STEP 4: Configure Build with CMake
###############################################################################
echo ""
echo -e "${GREEN}Step 4: CMake Configuration${NC}"
echo "-----------------------------------------------"

# Base CMake arguments
CMAKE_ARGS=(
    # Build type
    "-DCMAKE_BUILD_TYPE=${BUILD_TYPE}"
    
    # CUDA Configuration
    "-DLLAMA_CUDA=ON"
    "-DLLAMA_CUDA_F16=ON"
    "-DLLAMA_CUDA_FORCE_MMQ=ON"
    "-DCMAKE_CUDA_ARCHITECTURES=${CUDA_ARCHITECTURES}"
    
    # BLAS Configuration (OpenBLAS for CPU fallback)
    "-DLLAMA_BLAS=ON"
    "-DLLAMA_BLAS_VENDOR=OpenBLAS"
    
    # CPU Optimizations
    "-DLLAMA_NATIVE=ON"              # Enable native CPU optimizations
    "-DLLAMA_AVX=ON"                 # AVX support
    "-DLLAMA_AVX2=ON"                # AVX2 support
    "-DLLAMA_AVX512=OFF"             # Disable AVX512 (may not be available)
    "-DLLAMA_FMA=ON"                 # FMA support
    "-DLLAMA_F16C=ON"                # F16C support
    
    # Multi-threading
    "-DLLAMA_OPENMP=ON"              # Enable OpenMP
    
    # Quantization Support
    "-DLLAMA_QKK_64=ON"              # 64-bit quantization
    
    # Memory Management
    "-DLLAMA_CUDA_PEER_MAX_BATCH_SIZE=128"
    
    # Build options
    "-DBUILD_SHARED_LIBS=ON"         # Build shared libraries
    "-DCMAKE_POSITION_INDEPENDENT_CODE=ON"
    
    # Compiler optimizations
    "-DCMAKE_C_FLAGS=-O3 -march=native -mtune=native"
    "-DCMAKE_CXX_FLAGS=-O3 -march=native -mtune=native"
    "-DCMAKE_CUDA_FLAGS=-O3"
)

# Additional optimizations for specific GPUs
if [ -n "$CUDA_ARCHITECTURES" ]; then
    CMAKE_ARGS+=("-DLLAMA_CUDA_DMMV_X=32")
    CMAKE_ARGS+=("-DLLAMA_CUDA_DMMV_Y=1")
    CMAKE_ARGS+=("-DLLAMA_CUDA_MMV_Y=1")
fi

# Display configuration
echo -e "${BLUE}CMake Arguments:${NC}"
printf '%s\n' "${CMAKE_ARGS[@]}"

# Run CMake
echo ""
echo -e "${BLUE}Running CMake...${NC}"
cmake .. "${CMAKE_ARGS[@]}"

if [ $? -ne 0 ]; then
    echo -e "${RED}CMake configuration failed!${NC}"
    exit 1
fi

echo -e "${GREEN}CMake configuration successful${NC}"

###############################################################################
# STEP 5: Build
###############################################################################
echo ""
echo -e "${GREEN}Step 5: Building llama.cpp${NC}"
echo "-----------------------------------------------"

# Determine number of parallel jobs
NPROC=$(nproc)
JOBS=$((NPROC > 8 ? 8 : NPROC))  # Limit to 8 parallel jobs to avoid memory issues

echo -e "${BLUE}Building with $JOBS parallel jobs...${NC}"

# Build with verbose output for debugging
cmake --build . --config ${BUILD_TYPE} --parallel ${JOBS} --verbose

if [ $? -ne 0 ]; then
    echo -e "${RED}Build failed!${NC}"
    exit 1
fi

echo -e "${GREEN}Build successful!${NC}"

###############################################################################
# STEP 6: Verify Build
###############################################################################
echo ""
echo -e "${GREEN}Step 6: Build Verification${NC}"
echo "-----------------------------------------------"

# Check for main binaries
BINARIES=("llama-cli" "llama-server" "llama-bench")

for binary in "${BINARIES[@]}"; do
    if [ -f "bin/${binary}" ]; then
        echo -e "${GREEN}✓ ${binary} built successfully${NC}"
        
        # Check if CUDA is linked
        if ldd "bin/${binary}" 2>/dev/null | grep -q cuda; then
            echo -e "  ${BLUE}  CUDA linked${NC}"
        fi
    else
        echo -e "${YELLOW}⚠ ${binary} not found${NC}"
    fi
done

# Check for shared libraries
if [ -f "libllama.so" ]; then
    echo -e "${GREEN}✓ libllama.so built successfully${NC}"
fi

###############################################################################
# STEP 7: Install Libraries
###############################################################################
echo ""
echo -e "${GREEN}Step 7: Installing Libraries${NC}"
echo "-----------------------------------------------"

# Create library directory
LIB_DIR="${HOME}/.local/lib/helixllm"
mkdir -p "$LIB_DIR"

# Copy libraries
cp -f libllama.so "$LIB_DIR/" 2>/dev/null || true
cp -f libggml.so "$LIB_DIR/" 2>/dev/null || true
cp -f libggml-cuda.so "$LIB_DIR/" 2>/dev/null || true
cp -f libggml-base.so "$LIB_DIR/" 2>/dev/null || true

# Update library path
if ! grep -q "$LIB_DIR" "$HOME/.bashrc" 2>/dev/null; then
    echo "export LD_LIBRARY_PATH=${LIB_DIR}:\$LD_LIBRARY_PATH" >> "$HOME/.bashrc"
    echo -e "${GREEN}Library path added to .bashrc${NC}"
fi

export LD_LIBRARY_PATH="${LIB_DIR}:${LD_LIBRARY_PATH}"

echo -e "${GREEN}Libraries installed to $LIB_DIR${NC}"

###############################################################################
# STEP 8: Run Benchmark
###############################################################################
echo ""
echo -e "${GREEN}Step 8: Running Quick Benchmark${NC}"
echo "-----------------------------------------------"

if [ -f "bin/llama-bench" ]; then
    echo -e "${BLUE}Running llama-bench...${NC}"
    ./bin/llama-bench --help | head -20
else
    echo -e "${YELLOW}llama-bench not available${NC}"
fi

###############################################################################
# STEP 9: Create Build Info File
###############################################################################
echo ""
echo -e "${GREEN}Step 9: Creating Build Info${NC}"
echo "-----------------------------------------------"

cat > "${LLAMA_CPP_DIR}/build_info.txt" << EOF
llama.cpp Build Information
===========================
Build Date: $(date)
Build Type: ${BUILD_TYPE}
CUDA Architectures: ${CUDA_ARCHITECTURES}

CMake Arguments:
$(printf '%s\n' "${CMAKE_ARGS[@]}")

GPU Information:
$(nvidia-smi --query-gpu=name,compute_cap,memory.total --format=csv)

CPU Information:
$(cat /proc/cpuinfo | grep "model name" | head -n1)

Build Directory: ${LLAMA_CPP_DIR}/build
Library Directory: ${LIB_DIR}
EOF

echo -e "${GREEN}Build info saved to ${LLAMA_CPP_DIR}/build_info.txt${NC}"

###############################################################################
# Completion
###############################################################################
echo ""
echo -e "${GREEN}===============================================${NC}"
echo -e "${GREEN}llama.cpp Build Complete!${NC}"
echo -e "${GREEN}===============================================${NC}"
echo ""
echo "Build Location: ${LLAMA_CPP_DIR}/build"
echo "Binaries: ${LLAMA_CPP_DIR}/build/bin/"
echo "Libraries: ${LIB_DIR}"
echo ""
echo "Next steps:"
echo "  1. Install llama-cpp-python: ./03_install_llama_cpp_python.sh"
echo "  2. Download models: ./04_download_models.sh"
echo ""
