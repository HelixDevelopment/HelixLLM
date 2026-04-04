#!/usr/bin/env python3
"""
=============================================================================
Light Local LLM System - Integration Tests
=============================================================================
Integration tests for component interactions:
- RAG service with ChromaDB
- API Gateway with backend services
- MCP server with tools
- Service health checks

Usage:
    pytest tests/test_integration.py -v
    pytest tests/test_integration.py -m "slow" -v
=============================================================================
"""

import unittest
import requests
import time
import os
from typing import Optional
import pytest


# =============================================================================
# Configuration
# =============================================================================

OLLAMA_URL = os.getenv("OLLAMA_URL", "http://localhost:11434")
CHROMA_URL = os.getenv("CHROMA_URL", "http://localhost:8000")
RAG_URL = os.getenv("RAG_URL", "http://localhost:8001")
MCP_URL = os.getenv("MCP_URL", "http://localhost:3000")
API_URL = os.getenv("API_URL", "http://localhost:8080")


# =============================================================================
# Test Decorators
# =============================================================================

def requires_service(url: str, timeout: int = 5):
    """Decorator to skip tests if service is unavailable"""
    def decorator(func):
        def wrapper(*args, **kwargs):
            try:
                response = requests.get(f"{url}/health", timeout=timeout)
                if response.status_code == 200:
                    return func(*args, **kwargs)
            except requests.RequestException:
                pass
            pytest.skip(f"Service at {url} is not available")
        return wrapper
    return decorator


# =============================================================================
# Ollama Integration Tests
# =============================================================================

class TestOllamaIntegration(unittest.TestCase):
    """Integration tests for Ollama LLM service"""
    
    @classmethod
    def setUpClass(cls):
        """Check if Ollama is available"""
        cls.available = False
        try:
            response = requests.get(f"{OLLAMA_URL}/api/tags", timeout=5)
            cls.available = response.status_code == 200
        except:
            pass
            
    def setUp(self):
        """Skip if Ollama not available"""
        if not self.available:
            self.skipTest("Ollama service not available")
            
    def test_list_models(self):
        """Test listing available models"""
        response = requests.get(f"{OLLAMA_URL}/api/tags")
        self.assertEqual(response.status_code, 200)
        
        data = response.json()
        self.assertIn("models", data)
        
    def test_generate_completion(self):
        """Test text generation"""
        response = requests.post(
            f"{OLLAMA_URL}/api/generate",
            json={
                "model": "llama3.2",
                "prompt": "Say 'Hello, World!'",
                "stream": False
            },
            timeout=60
        )
        
        self.assertEqual(response.status_code, 200)
        data = response.json()
        self.assertIn("response", data)
        
    def test_chat_completion(self):
        """Test chat completion"""
        response = requests.post(
            f"{OLLAMA_URL}/api/chat",
            json={
                "model": "llama3.2",
                "messages": [
                    {"role": "user", "content": "Hello!"}
                ],
                "stream": False
            },
            timeout=60
        )
        
        self.assertEqual(response.status_code, 200)
        data = response.json()
        self.assertIn("message", data)


# =============================================================================
# ChromaDB Integration Tests
# =============================================================================

class TestChromaDBIntegration(unittest.TestCase):
    """Integration tests for ChromaDB vector database"""
    
    @classmethod
    def setUpClass(cls):
        """Check if ChromaDB is available"""
        cls.available = False
        try:
            response = requests.get(f"{CHROMA_URL}/api/v1/heartbeat", timeout=5)
            cls.available = response.status_code == 200
        except:
            pass
            
    def setUp(self):
        """Skip if ChromaDB not available"""
        if not self.available:
            self.skipTest("ChromaDB service not available")
            
    def test_heartbeat(self):
        """Test ChromaDB heartbeat"""
        response = requests.get(f"{CHROMA_URL}/api/v1/heartbeat")
        self.assertEqual(response.status_code, 200)
        
    def test_list_collections(self):
        """Test listing collections"""
        response = requests.get(f"{CHROMA_URL}/api/v1/collections")
        self.assertEqual(response.status_code, 200)
        
        data = response.json()
        self.assertIsInstance(data, list)
        
    def test_create_collection(self):
        """Test creating a collection"""
        collection_name = f"test_collection_{int(time.time())}"
        
        response = requests.post(
            f"{CHROMA_URL}/api/v1/collections",
            json={"name": collection_name}
        )
        
        self.assertEqual(response.status_code, 200)
        
        # Cleanup
        requests.delete(f"{CHROMA_URL}/api/v1/collections/{collection_name}")
        
    def test_add_and_query_documents(self):
        """Test adding and querying documents"""
        collection_name = f"test_docs_{int(time.time())}"
        
        # Create collection
        requests.post(
            f"{CHROMA_URL}/api/v1/collections",
            json={"name": collection_name}
        )
        
        # Add documents
        documents = {
            "ids": ["doc1", "doc2", "doc3"],
            "documents": [
                "Machine learning is a subset of AI.",
                "Deep learning uses neural networks.",
                "Natural language processing enables text understanding."
            ],
            "embeddings": [
                [0.1] * 384,
                [0.2] * 384,
                [0.3] * 384
            ]
        }
        
        response = requests.post(
            f"{CHROMA_URL}/api/v1/collections/{collection_name}/add",
            json=documents
        )
        
        self.assertEqual(response.status_code, 200)
        
        # Query documents
        query = {
            "query_embeddings": [[0.15] * 384],
            "n_results": 2
        }
        
        response = requests.post(
            f"{CHROMA_URL}/api/v1/collections/{collection_name}/query",
            json=query
        )
        
        self.assertEqual(response.status_code, 200)
        data = response.json()
        self.assertIn("ids", data)
        
        # Cleanup
        requests.delete(f"{CHROMA_URL}/api/v1/collections/{collection_name}")


# =============================================================================
# RAG Service Integration Tests
# =============================================================================

class TestRAGServiceIntegration(unittest.TestCase):
    """Integration tests for RAG service"""
    
    @classmethod
    def setUpClass(cls):
        """Check if RAG service is available"""
        cls.available = False
        try:
            response = requests.get(f"{RAG_URL}/health", timeout=5)
            cls.available = response.status_code == 200
        except:
            pass
            
    def setUp(self):
        """Skip if RAG service not available"""
        if not self.available:
            self.skipTest("RAG service not available")
            
    def test_health_check(self):
        """Test RAG service health"""
        response = requests.get(f"{RAG_URL}/health")
        self.assertEqual(response.status_code, 200)
        
        data = response.json()
        self.assertEqual(data.get("status"), "healthy")
        
    def test_index_document(self):
        """Test document indexing"""
        document = {
            "content": "Test document for indexing.",
            "metadata": {"source": "test", "type": "text"}
        }
        
        response = requests.post(
            f"{RAG_URL}/index",
            json=document
        )
        
        self.assertEqual(response.status_code, 200)
        data = response.json()
        self.assertIn("document_id", data)
        
    def test_query(self):
        """Test RAG query"""
        query = {
            "query": "What is machine learning?",
            "top_k": 3,
            "include_sources": True
        }
        
        response = requests.post(
            f"{RAG_URL}/query",
            json=query
        )
        
        self.assertEqual(response.status_code, 200)
        data = response.json()
        self.assertIn("response", data)
        
    def test_query_with_filters(self):
        """Test RAG query with metadata filters"""
        query = {
            "query": "AI concepts",
            "top_k": 5,
            "filters": {"type": "text"},
            "include_sources": True
        }
        
        response = requests.post(
            f"{RAG_URL}/query",
            json=query
        )
        
        self.assertEqual(response.status_code, 200)


# =============================================================================
# MCP Server Integration Tests
# =============================================================================

class TestMCPServerIntegration(unittest.TestCase):
    """Integration tests for MCP server"""
    
    @classmethod
    def setUpClass(cls):
        """Check if MCP server is available"""
        cls.available = False
        try:
            response = requests.get(f"{MCP_URL}/health", timeout=5)
            cls.available = response.status_code == 200
        except:
            pass
            
    def setUp(self):
        """Skip if MCP server not available"""
        if not self.available:
            self.skipTest("MCP server not available")
            
    def test_health_check(self):
        """Test MCP server health"""
        response = requests.get(f"{MCP_URL}/health")
        self.assertEqual(response.status_code, 200)
        
    def test_list_tools(self):
        """Test listing available tools"""
        response = requests.get(f"{MCP_URL}/tools")
        self.assertEqual(response.status_code, 200)
        
        data = response.json()
        self.assertIn("tools", data)
        
    def test_call_tool(self):
        """Test calling a tool"""
        # First get list of tools
        response = requests.get(f"{MCP_URL}/tools")
        tools = response.json().get("tools", [])
        
        if not tools:
            self.skipTest("No tools available")
            
        tool_name = tools[0]["name"]
        
        # Call the tool
        response = requests.post(
            f"{MCP_URL}/tools/{tool_name}",
            json={"input": "test input"}
        )
        
        # Should either succeed or return validation error
        self.assertIn(response.status_code, [200, 400, 422])


# =============================================================================
# API Gateway Integration Tests
# =============================================================================

class TestAPIGatewayIntegration(unittest.TestCase):
    """Integration tests for API Gateway"""
    
    @classmethod
    def setUpClass(cls):
        """Check if API Gateway is available"""
        cls.available = False
        try:
            response = requests.get(f"{API_URL}/health", timeout=5)
            cls.available = response.status_code == 200
        except:
            pass
            
    def setUp(self):
        """Skip if API Gateway not available"""
        if not self.available:
            self.skipTest("API Gateway not available")
            
    def test_health_check(self):
        """Test API Gateway health"""
        response = requests.get(f"{API_URL}/health")
        self.assertEqual(response.status_code, 200)
        
        data = response.json()
        self.assertEqual(data.get("status"), "healthy")
        
    def test_chat_endpoint(self):
        """Test chat endpoint"""
        request = {
            "message": "Hello, how are you?",
            "use_rag": False,
            "stream": False
        }
        
        response = requests.post(
            f"{API_URL}/chat",
            json=request,
            timeout=120
        )
        
        self.assertEqual(response.status_code, 200)
        data = response.json()
        self.assertIn("response", data)
        
    def test_chat_with_rag(self):
        """Test chat endpoint with RAG enabled"""
        request = {
            "message": "What is artificial intelligence?",
            "use_rag": True,
            "stream": False
        }
        
        response = requests.post(
            f"{API_URL}/chat",
            json=request,
            timeout=120
        )
        
        self.assertEqual(response.status_code, 200)
        data = response.json()
        self.assertIn("response", data)
        
    def test_invalid_request(self):
        """Test handling of invalid requests"""
        request = {
            # Missing required 'message' field
            "use_rag": True
        }
        
        response = requests.post(
            f"{API_URL}/chat",
            json=request
        )
        
        self.assertEqual(response.status_code, 400)
        
    def test_rate_limit_headers(self):
        """Test rate limit headers in response"""
        response = requests.get(f"{API_URL}/health")
        
        # Check for rate limit headers
        self.assertIn("X-RateLimit-Limit", response.headers or {})
        self.assertIn("X-RateLimit-Remaining", response.headers or {})


# =============================================================================
# End-to-End Integration Tests
# =============================================================================

class TestEndToEndIntegration(unittest.TestCase):
    """End-to-end integration tests"""
    
    @pytest.mark.slow
    def test_full_rag_pipeline(self):
        """Test complete RAG pipeline"""
        # Check all services are available
        services = [
            (OLLAMA_URL, "/api/tags", "Ollama"),
            (CHROMA_URL, "/api/v1/heartbeat", "ChromaDB"),
            (RAG_URL, "/health", "RAG Service"),
            (API_URL, "/health", "API Gateway")
        ]
        
        for url, endpoint, name in services:
            try:
                response = requests.get(f"{url}{endpoint}", timeout=5)
                self.assertEqual(response.status_code, 200, f"{name} not available")
            except requests.RequestException:
                self.skipTest(f"{name} not available")
                
        # Test end-to-end query
        request = {
            "message": "Explain the concept of machine learning.",
            "use_rag": True,
            "stream": False
        }
        
        response = requests.post(
            f"{API_URL}/chat",
            json=request,
            timeout=180
        )
        
        self.assertEqual(response.status_code, 200)
        data = response.json()
        
        self.assertIn("response", data)
        self.assertTrue(len(data["response"]) > 0)
        
        if "sources" in data:
            self.assertIsInstance(data["sources"], list)
            
    @pytest.mark.slow
    def test_concurrent_requests(self):
        """Test handling of concurrent requests"""
        import concurrent.futures
        
        def make_request(i):
            try:
                response = requests.post(
                    f"{API_URL}/chat",
                    json={"message": f"Test message {i}", "use_rag": False, "stream": False},
                    timeout=60
                )
                return response.status_code == 200
            except:
                return False
                
        with concurrent.futures.ThreadPoolExecutor(max_workers=5) as executor:
            futures = [executor.submit(make_request, i) for i in range(10)]
            results = [f.result() for f in concurrent.futures.as_completed(futures)]
            
        # At least 80% of requests should succeed
        success_rate = sum(results) / len(results)
        self.assertGreaterEqual(success_rate, 0.8)


# =============================================================================
# Main Entry Point
# =============================================================================

if __name__ == '__main__':
    unittest.main(verbosity=2)
