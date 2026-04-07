#!/bin/sh
set -e

# Download model if not present
/usr/local/bin/download-model

# Start llama-server with optimized settings
# RPC support: if LLAMA_RPC_SERVERS is set, enable distributed inference
RPC_ARGS=""
if [ -n "${LLAMA_RPC_SERVERS}" ]; then
    RPC_ARGS="--rpc ${LLAMA_RPC_SERVERS}"
    echo "RPC distributed inference enabled: ${LLAMA_RPC_SERVERS}"
fi

# Override context length to support large system prompts (CLAUDE.md ~60K tokens)
CTX_OVERRIDE=""
if [ -n "${LLAMA_CTX_OVERRIDE}" ]; then
    CTX_OVERRIDE="--override-kv qwen2.context_length=int:${LLAMA_CTX_OVERRIDE}"
fi

exec llama-server \
    --host "${LLAMA_HOST:-0.0.0.0}" \
    --port "${LLAMA_PORT:-8080}" \
    --model "${MODEL_PATH:-/models/model.gguf}" \
    --ctx-size "${LLAMA_CTX_SIZE:-65536}" \
    --threads "${LLAMA_THREADS:-4}" \
    --threads-batch "${LLAMA_THREADS_BATCH:-4}" \
    --batch-size "${LLAMA_BATCH_SIZE:-4096}" \
    --n-predict "${LLAMA_N_PREDICT:--1}" \
    --parallel "${LLAMA_PARALLEL:-1}" \
    --jinja \
    --cont-batching \
    --metrics \
    $CTX_OVERRIDE \
    $RPC_ARGS \
    "$@"
