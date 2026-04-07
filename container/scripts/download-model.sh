#!/bin/sh
set -e

MODEL_URL="${MODEL_URL:-https://huggingface.co/bartowski/Qwen2.5-1.5B-Instruct-GGUF/resolve/main/Qwen2.5-1.5B-Instruct-Q4_K_M.gguf}"
MODEL_PATH="${MODEL_PATH:-/models/model.gguf}"

if [ -f "$MODEL_PATH" ]; then
    echo "Model already exists at $MODEL_PATH"
    exit 0
fi

echo "Downloading model from $MODEL_URL..."
curl -L --progress-bar -o "$MODEL_PATH.tmp" "$MODEL_URL"
mv "$MODEL_PATH.tmp" "$MODEL_PATH"
echo "Model downloaded to $MODEL_PATH"
