#!/usr/bin/env python3
"""
HelixLLM Model Download Script
Downloads optimized models for consumer hardware
"""

import os
import sys
import argparse
from pathlib import Path
from typing import Dict, List, Optional
import urllib.request
import hashlib
from tqdm import tqdm


# Model registry with optimized models for 6GB VRAM
MODEL_REGISTRY = {
    "qwen2.5-1.5b-instruct-q4_k_m": {
        "name": "Qwen2.5-1.5B-Instruct-Q4_K_M",
        "description": "1.5B parameter instruction-tuned model (Q4_K_M quantized)",
        "url": "https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct-GGUF/resolve/main/qwen2.5-1.5b-instruct-q4_k_m.gguf",
        "filename": "Qwen2.5-1.5B-Instruct-Q4_K_M.gguf",
        "size_gb": 1.0,
        "sha256": None,  # Add if available
        "recommended": True,
    },
    "qwen2.5-1.5b-instruct-q5_k_m": {
        "name": "Qwen2.5-1.5B-Instruct-Q5_K_M",
        "description": "1.5B parameter instruction-tuned model (Q5_K_M quantized, higher quality)",
        "url": "https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct-GGUF/resolve/main/qwen2.5-1.5b-instruct-q5_k_m.gguf",
        "filename": "Qwen2.5-1.5B-Instruct-Q5_K_M.gguf",
        "size_gb": 1.2,
        "sha256": None,
        "recommended": False,
    },
    "qwen2.5-3b-instruct-q4_k_m": {
        "name": "Qwen2.5-3B-Instruct-Q4_K_M",
        "description": "3B parameter instruction-tuned model (Q4_K_M quantized)",
        "url": "https://huggingface.co/Qwen/Qwen2.5-3B-Instruct-GGUF/resolve/main/qwen2.5-3b-instruct-q4_k_m.gguf",
        "filename": "Qwen2.5-3B-Instruct-Q4_K_M.gguf",
        "size_gb": 1.9,
        "sha256": None,
        "recommended": False,
    },
    "nomic-embed-text-v1.5-q4_k_m": {
        "name": "nomic-embed-text-v1.5.Q4_K_M",
        "description": "High-quality embedding model (Q4_K_M quantized)",
        "url": "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q4_K_M.gguf",
        "filename": "nomic-embed-text-v1.5.Q4_K_M.gguf",
        "size_gb": 0.3,
        "sha256": None,
        "recommended": True,
    },
    "nomic-embed-text-v1.5-f16": {
        "name": "nomic-embed-text-v1.5.f16",
        "description": "High-quality embedding model (FP16, best quality)",
        "url": "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.f16.gguf",
        "filename": "nomic-embed-text-v1.5.f16.gguf",
        "size_gb": 0.5,
        "sha256": None,
        "recommended": False,
    },
}


class DownloadProgressBar(tqdm):
    """Progress bar for downloads"""
    def update_to(self, b=1, bsize=1, tsize=None):
        if tsize is not None:
            self.total = tsize
        self.update(b * bsize - self.n)


def download_file(url: str, output_path: str, desc: str = "Downloading"):
    """Download file with progress bar"""
    print(f"\n{desc}...")
    print(f"URL: {url}")
    print(f"Output: {output_path}")
    
    # Create directory if needed
    os.makedirs(os.path.dirname(output_path), exist_ok=True)
    
    # Download with progress bar
    with DownloadProgressBar(unit='B', unit_scale=True, miniters=1, desc=desc) as t:
        urllib.request.urlretrieve(url, filename=output_path, reporthook=t.update_to)
    
    print(f"✓ Download complete: {output_path}")
    
    # Verify file size
    file_size = os.path.getsize(output_path) / (1024**3)  # Convert to GB
    print(f"  Size: {file_size:.2f} GB")


def verify_checksum(file_path: str, expected_sha256: str) -> bool:
    """Verify file SHA256 checksum"""
    if not expected_sha256:
        return True
    
    print(f"Verifying checksum...")
    sha256_hash = hashlib.sha256()
    
    with open(file_path, "rb") as f:
        for byte_block in iter(lambda: f.read(4096), b""):
            sha256_hash.update(byte_block)
    
    actual_hash = sha256_hash.hexdigest()
    
    if actual_hash.lower() == expected_sha256.lower():
        print("✓ Checksum verified")
        return True
    else:
        print(f"✗ Checksum mismatch!")
        print(f"  Expected: {expected_sha256}")
        print(f"  Actual:   {actual_hash}")
        return False


def list_models():
    """List available models"""
    print("\n" + "="*70)
    print("         Available Models")
    print("="*70 + "\n")
    
    for key, model in MODEL_REGISTRY.items():
        recommended = " [RECOMMENDED]" if model.get("recommended") else ""
        print(f"{key}{recommended}")
        print(f"  Name: {model['name']}")
        print(f"  Description: {model['description']}")
        print(f"  Size: {model['size_gb']:.1f} GB")
        print()


def download_model(model_key: str, output_dir: str = "models") -> str:
    """Download a specific model"""
    if model_key not in MODEL_REGISTRY:
        print(f"Error: Model '{model_key}' not found")
        print(f"Run with --list to see available models")
        return None
    
    model = MODEL_REGISTRY[model_key]
    output_path = os.path.join(output_dir, model['filename'])
    
    # Check if already exists
    if os.path.exists(output_path):
        print(f"\nModel already exists: {output_path}")
        response = input("Redownload? (y/n): ").lower()
        if response != 'y':
            return output_path
    
    # Download
    try:
        download_file(
            url=model['url'],
            output_path=output_path,
            desc=f"Downloading {model['name']}"
        )
        
        # Verify checksum if available
        if model.get('sha256'):
            if not verify_checksum(output_path, model['sha256']):
                print("Warning: Checksum verification failed")
        
        return output_path
        
    except Exception as e:
        print(f"Error downloading model: {e}")
        # Cleanup partial download
        if os.path.exists(output_path):
            os.remove(output_path)
        return None


def download_all_recommended(output_dir: str = "models"):
    """Download all recommended models"""
    print("\n" + "="*70)
    print("         Downloading Recommended Models")
    print("="*70)
    
    downloaded = []
    
    for key, model in MODEL_REGISTRY.items():
        if model.get("recommended"):
            path = download_model(key, output_dir)
            if path:
                downloaded.append(path)
    
    print("\n" + "="*70)
    print(f"Downloaded {len(downloaded)} models")
    print("="*70)
    
    return downloaded


def main():
    parser = argparse.ArgumentParser(description="HelixLLM Model Downloader")
    parser.add_argument("--list", action="store_true",
                       help="List available models")
    parser.add_argument("--download", type=str,
                       help="Download specific model by key")
    parser.add_argument("--download-all", action="store_true",
                       help="Download all recommended models")
    parser.add_argument("--output-dir", type=str, default="models",
                       help="Output directory for models")
    
    args = parser.parse_args()
    
    # Create output directory
    os.makedirs(args.output_dir, exist_ok=True)
    
    if args.list:
        list_models()
    elif args.download:
        download_model(args.download, args.output_dir)
    elif args.download_all:
        download_all_recommended(args.output_dir)
    else:
        # Default: download recommended models
        print("No action specified. Downloading recommended models...")
        print("Use --help for more options")
        download_all_recommended(args.output_dir)


if __name__ == "__main__":
    main()
