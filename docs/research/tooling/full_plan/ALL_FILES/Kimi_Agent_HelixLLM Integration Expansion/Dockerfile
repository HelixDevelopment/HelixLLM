# HelixLLM + HelixAgent Dockerfile
# ================================
# Multi-stage build for optimized production image

# ============================================================================
# Stage 1: Builder
# ============================================================================
FROM nvidia/cuda:12.1.0-devel-ubuntu22.04 AS builder

# Prevent interactive prompts during build
ENV DEBIAN_FRONTEND=noninteractive

# Install build dependencies
RUN apt-get update && apt-get install -y \
    python3.11 \
    python3.11-dev \
    python3.11-venv \
    python3-pip \
    git \
    build-essential \
    cmake \
    ninja-build \
    && rm -rf /var/lib/apt/lists/*

# Set Python 3.11 as default
RUN update-alternatives --install /usr/bin/python python /usr/bin/python3.11 1
RUN update-alternatives --install /usr/bin/pip pip /usr/bin/pip3 1

# Create virtual environment
RUN python -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

# Upgrade pip
RUN pip install --no-cache-dir --upgrade pip setuptools wheel

# Install PyTorch with CUDA support
RUN pip install --no-cache-dir \
    torch==2.1.2 \
    torchvision \
    torchaudio \
    --index-url https://download.pytorch.org/whl/cu121

# Install llama-cpp-python with CUDA support
ENV CMAKE_ARGS="-DLLAMA_CUDA=on -DLLAMA_CUDA_FORCE_MMQ=ON"
RUN pip install --no-cache-dir llama-cpp-python==0.2.29

# Install other Python dependencies
COPY requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt

# ============================================================================
# Stage 2: Production
# ============================================================================
FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04 AS production

# Prevent interactive prompts
ENV DEBIAN_FRONTEND=noninteractive

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    python3.11 \
    python3.11-venv \
    curl \
    libgomp1 \
    && rm -rf /var/lib/apt/lists/*

# Set Python 3.11 as default
RUN update-alternatives --install /usr/bin/python python /usr/bin/python3.11 1

# Copy virtual environment from builder
COPY --from=builder /opt/venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

# Set working directory
WORKDIR /app

# Create necessary directories
RUN mkdir -p /app/data /app/logs /app/models

# Copy application code
COPY helixllm/ /app/helixllm/
COPY helixagent/ /app/helixagent/
COPY helix_interfaces.py /app/
COPY config.yaml /app/

# Copy entrypoint script
COPY docker-entrypoint.sh /app/
RUN chmod +x /app/docker-entrypoint.sh

# Environment variables
ENV PYTHONPATH=/app
ENV PYTHONUNBUFFERED=1
ENV HELIX_CONFIG_PATH=/app/config.yaml

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD curl -f http://localhost:8000/health || exit 1

# Expose ports
EXPOSE 8000

# Set entrypoint
ENTRYPOINT ["/app/docker-entrypoint.sh"]

# Default command
CMD ["server"]

# ============================================================================
# Stage 3: Development (optional)
# ============================================================================
FROM production AS development

# Install development dependencies
RUN pip install --no-cache-dir \
    pytest \
    pytest-asyncio \
    black \
    isort \
    flake8 \
    mypy \
    ipython

# Mount source code as volume for live reload
VOLUME ["/app"]

CMD ["python", "-m", "helixllm.server", "--reload"]
