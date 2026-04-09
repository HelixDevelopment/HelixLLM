#!/usr/bin/env python3
"""
HelixLLM Server - Production-ready inference server
Optimized for consumer hardware with 6GB VRAM
"""

import os
import sys
import json
import time
import signal
import argparse
import threading
from typing import Dict, Any, Optional, List
from dataclasses import dataclass
from pathlib import Path
from datetime import datetime

# FastAPI for REST API
try:
    from fastapi import FastAPI, HTTPException, BackgroundTasks
    from fastapi.responses import StreamingResponse
    from pydantic import BaseModel
    FASTAPI_AVAILABLE = True
except ImportError:
    FASTAPI_AVAILABLE = False
    print("Warning: FastAPI not available. Install with: pip install fastapi uvicorn")

# Import HelixLLM components
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from 04_model_loader import HelixLLM, ModelConfig
from 05_runtime_config import RuntimeConfig, PresetConfigs
from 06_hardware_detection import HardwareDetector, AutoConfigurator
from 07_performance_monitor import PerformanceMonitor
from 08_optimization_checklist import OptimizationChecklist


# ============================================================================
# API Models
# ============================================================================

class GenerateRequest(BaseModel):
    prompt: str
    max_tokens: int = 256
    temperature: float = 0.7
    top_p: float = 0.9
    top_k: int = 40
    repeat_penalty: float = 1.1
    stream: bool = False


class EmbedRequest(BaseModel):
    texts: List[str]
    batch_size: int = 32


class HealthResponse(BaseModel):
    status: str
    models_loaded: Dict[str, bool]
    performance: Dict[str, Any]


# ============================================================================
# HelixLLM Server
# ============================================================================

class HelixLLMServer:
    """Production-ready HelixLLM server"""
    
    def __init__(self):
        self.helix = HelixLLM()
        self.monitor = PerformanceMonitor()
        self.config: Optional[RuntimeConfig] = None
        self.app: Optional[Any] = None
        self.running = False
        
        # Statistics
        self.stats = {
            'requests_total': 0,
            'tokens_generated': 0,
            'avg_tokens_per_second': 0.0,
            'start_time': None,
        }
    
    def initialize(self, 
                   llm_model_path: str = None,
                   embedding_model_path: str = None,
                   config: RuntimeConfig = None,
                   auto_detect: bool = True):
        """Initialize the server"""
        print("\n" + "="*60)
        print("         HelixLLM Server Initialization")
        print("="*60 + "\n")
        
        # Run optimization checklist
        checklist = OptimizationChecklist()
        checklist.run_pre_run_checks()
        
        if not checklist.is_ready():
            print("\n⚠ System not fully optimized. Continue anyway? (y/n)")
            response = input().lower()
            if response != 'y':
                return False
        
        # Apply runtime optimizations
        checklist.apply_runtime_optimizations()
        
        # Auto-detect hardware and configure
        if auto_detect and config is None:
            print("\n[Auto-Configuration]")
            detector = HardwareDetector()
            detector.print_summary()
            
            configurator = AutoConfigurator(detector.get_profile())
            auto_config = configurator.generate_config()
            
            # Use appropriate preset
            gpu_mem_gb = detector.get_profile().gpu_memory_total_mb / 1024
            if gpu_mem_gb >= 8:
                config = PresetConfigs.consumer_8gb()
            elif gpu_mem_gb >= 6:
                config = PresetConfigs.consumer_6gb()
            else:
                config = PresetConfigs.consumer_6gb()
        
        self.config = config or PresetConfigs.consumer_6gb()
        
        # Initialize HelixLLM
        print("\n[Loading Models]")
        llm_config = ModelConfig(
            n_gpu_layers=self.config.gpu_layers,
            n_ctx=self.config.context_size,
            n_batch=self.config.batch_size,
            n_ubatch=self.config.ubatch_size,
            n_threads=self.config.n_threads,
            n_threads_batch=self.config.n_threads_batch,
            use_mmap=self.config.use_mmap,
            use_mlock=self.config.use_mlock,
            offload_kqv=self.config.offload_kqv,
            flash_attn=self.config.flash_attn,
            cache_size=self.config.cache_size,
        )
        
        embedding_config = ModelConfig(
            n_gpu_layers=self.config.gpu_layers,
            n_ctx=2048,
            n_batch=1024,
            embedding=True,
        )
        
        self.helix.initialize(
            llm_path=llm_model_path,
            embedding_path=embedding_model_path,
            llm_config=llm_config,
            embedding_config=embedding_config,
        )
        
        self.stats['start_time'] = time.time()
        print("\n✓ Server initialized successfully")
        
        return True
    
    def create_api(self) -> Any:
        """Create FastAPI application"""
        if not FASTAPI_AVAILABLE:
            raise RuntimeError("FastAPI not available")
        
        app = FastAPI(
            title="HelixLLM Server",
            description="High-performance LLM inference server",
            version="1.0.0"
        )
        
        @app.get("/health", response_model=HealthResponse)
        async def health():
            """Health check endpoint"""
            uptime = time.time() - self.stats['start_time'] if self.stats['start_time'] else 0
            
            return HealthResponse(
                status="healthy",
                models_loaded={
                    'llm': self.helix.llm is not None,
                    'embedding': self.helix.embedder is not None,
                },
                performance={
                    'requests_total': self.stats['requests_total'],
                    'tokens_generated': self.stats['tokens_generated'],
                    'avg_tokens_per_second': self.stats['avg_tokens_per_second'],
                    'uptime_seconds': uptime,
                }
            )
        
        @app.post("/generate")
        async def generate(request: GenerateRequest):
            """Generate text from prompt"""
            if not self.helix.llm:
                raise HTTPException(status_code=503, detail="LLM not loaded")
            
            self.stats['requests_total'] += 1
            
            try:
                self.monitor.start_generation()
                
                result = self.helix.generate(
                    prompt=request.prompt,
                    max_tokens=request.max_tokens,
                    temperature=request.temperature,
                    top_p=request.top_p,
                    top_k=request.top_k,
                    repeat_penalty=request.repeat_penalty,
                    stream=request.stream,
                )
                
                self.monitor.end_generation()
                
                # Update statistics
                self.stats['tokens_generated'] += result['tokens_generated']
                if self.stats['requests_total'] > 0:
                    self.stats['avg_tokens_per_second'] = (
                        (self.stats['avg_tokens_per_second'] * (self.stats['requests_total'] - 1) +
                         result['tokens_per_second']) / self.stats['requests_total']
                    )
                
                return {
                    'text': result['text'],
                    'tokens_generated': result['tokens_generated'],
                    'tokens_per_second': result['tokens_per_second'],
                    'generation_time': result['generation_time'],
                }
                
            except Exception as e:
                raise HTTPException(status_code=500, detail=str(e))
        
        @app.post("/embed")
        async def embed(request: EmbedRequest):
            """Generate embeddings for texts"""
            if not self.helix.embedder:
                raise HTTPException(status_code=503, detail="Embedding model not loaded")
            
            try:
                embeddings = self.helix.embed(
                    texts=request.texts,
                    batch_size=request.batch_size
                )
                
                return {
                    'embeddings': embeddings,
                    'count': len(embeddings),
                    'dimension': len(embeddings[0]) if embeddings else 0,
                }
                
            except Exception as e:
                raise HTTPException(status_code=500, detail=str(e))
        
        @app.get("/stats")
        async def stats():
            """Get server statistics"""
            return {
                'requests_total': self.stats['requests_total'],
                'tokens_generated': self.stats['tokens_generated'],
                'avg_tokens_per_second': self.stats['avg_tokens_per_second'],
                'uptime_seconds': time.time() - self.stats['start_time'] if self.stats['start_time'] else 0,
            }
        
        self.app = app
        return app
    
    def run(self, host: str = "0.0.0.0", port: int = 8080):
        """Run the server"""
        if not FASTAPI_AVAILABLE:
            print("Error: FastAPI not available")
            return
        
        if not self.app:
            self.create_api()
        
        import uvicorn
        
        print(f"\n{'='*60}")
        print(f"         HelixLLM Server Running")
        print(f"{'='*60}")
        print(f"  API: http://{host}:{port}")
        print(f"  Health: http://{host}:{port}/health")
        print(f"{'='*60}\n")
        
        self.running = True
        
        # Setup signal handlers
        def signal_handler(sig, frame):
            print("\nShutting down...")
            self.shutdown()
            sys.exit(0)
        
        signal.signal(signal.SIGINT, signal_handler)
        signal.signal(signal.SIGTERM, signal_handler)
        
        # Run server
        uvicorn.run(self.app, host=host, port=port, log_level="info")
    
    def shutdown(self):
        """Shutdown the server"""
        self.running = False
        
        # Run cleanup
        checklist = OptimizationChecklist()
        checklist.run_post_run_cleanup()
        
        # Unload models
        if self.helix:
            self.helix.shutdown()
        
        print("\n✓ Server shutdown complete")


# ============================================================================
# Command Line Interface
# ============================================================================

def main():
    parser = argparse.ArgumentParser(description="HelixLLM Server")
    parser.add_argument("--llm-model", type=str, 
                       default="models/Qwen2.5-1.5B-Instruct-Q4_K_M.gguf",
                       help="Path to LLM model")
    parser.add_argument("--embedding-model", type=str,
                       default="models/nomic-embed-text-v1.5.Q4_K_M.gguf",
                       help="Path to embedding model")
    parser.add_argument("--host", type=str, default="0.0.0.0",
                       help="Host to bind to")
    parser.add_argument("--port", type=int, default=8080,
                       help="Port to bind to")
    parser.add_argument("--config", type=str,
                       help="Path to configuration file")
    parser.add_argument("--profile", type=str,
                       choices=['consumer_6gb', 'consumer_8gb', 'consumer_12gb', 'cpu_only'],
                       help="Configuration profile")
    parser.add_argument("--no-auto-detect", action="store_true",
                       help="Disable hardware auto-detection")
    
    args = parser.parse_args()
    
    # Load configuration
    config = None
    if args.config:
        from 05_runtime_config import RuntimeConfig
        config = RuntimeConfig.load(args.config)
    elif args.profile:
        config = getattr(PresetConfigs, args.profile)()
    
    # Create and initialize server
    server = HelixLLMServer()
    
    success = server.initialize(
        llm_model_path=args.llm_model,
        embedding_model_path=args.embedding_model,
        config=config,
        auto_detect=not args.no_auto_detect,
    )
    
    if not success:
        print("\n✗ Server initialization failed")
        sys.exit(1)
    
    # Run server
    server.run(host=args.host, port=args.port)


if __name__ == "__main__":
    main()
