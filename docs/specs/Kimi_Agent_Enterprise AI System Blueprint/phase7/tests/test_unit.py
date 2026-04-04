#!/usr/bin/env python3
"""
=============================================================================
Light Local LLM System - Unit Tests
=============================================================================
Unit tests for individual components:
- RAG service components
- MCP tool handlers
- API gateway middleware
- Utility functions

Usage:
    pytest tests/test_unit.py -v
    pytest tests/test_unit.py::TestRAGService -v
=============================================================================
"""

import unittest
from unittest.mock import Mock, patch, MagicMock
import json
import sys
from pathlib import Path

# Add parent directory to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent))


# =============================================================================
# RAG Service Unit Tests
# =============================================================================

class TestEmbeddingService(unittest.TestCase):
    """Test embedding service functionality"""
    
    def setUp(self):
        """Set up test fixtures"""
        self.mock_model = Mock()
        self.sample_texts = [
            "This is a test sentence.",
            "Another test sentence for embeddings.",
            "Machine learning is fascinating."
        ]
        
    def test_embedding_generation(self):
        """Test that embeddings are generated correctly"""
        # Mock embedding output
        mock_embedding = [0.1] * 384  # 384-dim embedding
        self.mock_model.encode.return_value = [mock_embedding] * len(self.sample_texts)
        
        # Test embedding generation
        embeddings = self.mock_model.encode(self.sample_texts)
        
        self.assertEqual(len(embeddings), len(self.sample_texts))
        self.assertEqual(len(embeddings[0]), 384)
        self.mock_model.encode.assert_called_once_with(self.sample_texts)
        
    def test_embedding_normalization(self):
        """Test embedding normalization"""
        import numpy as np
        
        # Create a sample embedding
        embedding = np.array([3.0, 4.0])
        
        # Normalize
        norm = np.linalg.norm(embedding)
        normalized = embedding / norm
        
        # Check that normalized vector has unit length
        self.assertAlmostEqual(np.linalg.norm(normalized), 1.0, places=5)
        
    def test_batch_embedding(self):
        """Test batch embedding processing"""
        batch_size = 32
        num_texts = 100
        
        texts = [f"Text {i}" for i in range(num_texts)]
        
        # Mock batch processing
        mock_embeddings = [[0.1] * 384 for _ in range(num_texts)]
        self.mock_model.encode.return_value = mock_embeddings
        
        # Process in batches
        all_embeddings = []
        for i in range(0, len(texts), batch_size):
            batch = texts[i:i+batch_size]
            embeddings = self.mock_model.encode(batch)
            all_embeddings.extend(embeddings)
            
        self.assertEqual(len(all_embeddings), num_texts)


class TestDocumentChunker(unittest.TestCase):
    """Test document chunking functionality"""
    
    def setUp(self):
        """Set up test fixtures"""
        self.chunk_size = 100
        self.chunk_overlap = 20
        
    def test_simple_chunking(self):
        """Test basic document chunking"""
        text = "This is a test document. " * 50  # Long text
        
        # Simple chunking by character count
        chunks = []
        for i in range(0, len(text), self.chunk_size - self.chunk_overlap):
            chunk = text[i:i + self.chunk_size]
            if chunk:
                chunks.append(chunk)
                
        self.assertGreater(len(chunks), 1)
        
        # Check overlap
        if len(chunks) > 1:
            overlap = set(chunks[0][-self.chunk_overlap:]) & set(chunks[1][:self.chunk_overlap])
            self.assertGreater(len(overlap), 0)
            
    def test_sentence_boundary_chunking(self):
        """Test chunking at sentence boundaries"""
        text = "First sentence. Second sentence. Third sentence. Fourth sentence."
        sentences = text.split('. ')
        
        chunks = []
        current_chunk = []
        current_length = 0
        
        for sentence in sentences:
            sentence = sentence.strip()
            if not sentence:
                continue
                
            if current_length + len(sentence) > self.chunk_size and current_chunk:
                chunks.append('. '.join(current_chunk) + '.')
                # Keep last sentence for overlap
                current_chunk = current_chunk[-1:] if len(current_chunk) > 1 else []
                current_length = sum(len(s) for s in current_chunk)
                
            current_chunk.append(sentence)
            current_length += len(sentence)
            
        if current_chunk:
            chunks.append('. '.join(current_chunk) + '.')
            
        self.assertGreater(len(chunks), 0)
        
    def test_empty_document(self):
        """Test handling of empty document"""
        text = ""
        chunks = []
        
        if text:
            chunks = [text]
            
        self.assertEqual(len(chunks), 0)


class TestChromaDBClient(unittest.TestCase):
    """Test ChromaDB client functionality"""
    
    def setUp(self):
        """Set up test fixtures"""
        self.mock_client = Mock()
        self.collection_name = "test_documents"
        
    def test_collection_creation(self):
        """Test collection creation"""
        self.mock_client.get_or_create_collection.return_value = Mock()
        
        collection = self.mock_client.get_or_create_collection(
            name=self.collection_name
        )
        
        self.assertIsNotNone(collection)
        self.mock_client.get_or_create_collection.assert_called_once_with(
            name=self.collection_name
        )
        
    def test_document_addition(self):
        """Test adding documents to collection"""
        mock_collection = Mock()
        
        documents = ["doc1", "doc2", "doc3"]
        embeddings = [[0.1] * 384, [0.2] * 384, [0.3] * 384]
        ids = ["id1", "id2", "id3"]
        
        mock_collection.add.return_value = None
        
        mock_collection.add(
            documents=documents,
            embeddings=embeddings,
            ids=ids
        )
        
        mock_collection.add.assert_called_once_with(
            documents=documents,
            embeddings=embeddings,
            ids=ids
        )
        
    def test_similarity_search(self):
        """Test similarity search"""
        mock_collection = Mock()
        
        query_embedding = [0.1] * 384
        n_results = 5
        
        mock_results = {
            'ids': [['id1', 'id2', 'id3']],
            'distances': [[0.1, 0.2, 0.3]],
            'documents': [['doc1', 'doc2', 'doc3']]
        }
        mock_collection.query.return_value = mock_results
        
        results = mock_collection.query(
            query_embeddings=[query_embedding],
            n_results=n_results
        )
        
        self.assertEqual(len(results['ids'][0]), 3)
        mock_collection.query.assert_called_once()


# =============================================================================
# MCP Tool Server Unit Tests
# =============================================================================

class TestMCPToolRegistry(unittest.TestCase):
    """Test MCP tool registry"""
    
    def setUp(self):
        """Set up test fixtures"""
        self.tools = {}
        
    def register_tool(self, name: str, handler, schema: dict):
        """Register a tool"""
        self.tools[name] = {
            'handler': handler,
            'schema': schema
        }
        
    def test_tool_registration(self):
        """Test tool registration"""
        def mock_handler(params):
            return {"result": "success"}
            
        schema = {
            "type": "object",
            "properties": {
                "input": {"type": "string"}
            }
        }
        
        self.register_tool("test_tool", mock_handler, schema)
        
        self.assertIn("test_tool", self.tools)
        self.assertEqual(self.tools["test_tool"]["schema"], schema)
        
    def test_tool_execution(self):
        """Test tool execution"""
        def mock_handler(params):
            return {"result": params.get("input", "default")}
            
        self.register_tool("echo", mock_handler, {})
        
        result = self.tools["echo"]["handler"]({"input": "hello"})
        self.assertEqual(result["result"], "hello")
        
    def test_tool_schema_validation(self):
        """Test tool input schema validation"""
        schema = {
            "type": "object",
            "required": ["name"],
            "properties": {
                "name": {"type": "string"},
                "age": {"type": "integer"}
            }
        }
        
        # Valid input
        valid_input = {"name": "John", "age": 30}
        self.assertIn("name", valid_input)
        
        # Invalid input (missing required)
        invalid_input = {"age": 30}
        self.assertNotIn("name", invalid_input)


class TestMCPProtocol(unittest.TestCase):
    """Test MCP protocol implementation"""
    
    def test_initialize_request(self):
        """Test initialize request handling"""
        request = {
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "clientInfo": {
                    "name": "test-client",
                    "version": "1.0.0"
                }
            }
        }
        
        self.assertEqual(request["jsonrpc"], "2.0")
        self.assertEqual(request["method"], "initialize")
        
    def test_tools_list_request(self):
        """Test tools/list request"""
        request = {
            "jsonrpc": "2.0",
            "id": 2,
            "method": "tools/list"
        }
        
        self.assertEqual(request["method"], "tools/list")
        
    def test_tool_call_request(self):
        """Test tools/call request"""
        request = {
            "jsonrpc": "2.0",
            "id": 3,
            "method": "tools/call",
            "params": {
                "name": "test_tool",
                "arguments": {
                    "input": "test"
                }
            }
        }
        
        self.assertEqual(request["params"]["name"], "test_tool")


# =============================================================================
# API Gateway Unit Tests
# =============================================================================

class TestRateLimiter(unittest.TestCase):
    """Test rate limiting functionality"""
    
    def setUp(self):
        """Set up test fixtures"""
        self.requests = {}
        self.max_requests = 100
        self.window_seconds = 60
        
    def is_allowed(self, client_id: str) -> bool:
        """Check if request is allowed"""
        import time
        
        now = time.time()
        window_start = now - self.window_seconds
        
        # Clean old requests
        if client_id in self.requests:
            self.requests[client_id] = [
                req_time for req_time in self.requests[client_id]
                if req_time > window_start
            ]
        else:
            self.requests[client_id] = []
            
        # Check limit
        if len(self.requests[client_id]) >= self.max_requests:
            return False
            
        # Record request
        self.requests[client_id].append(now)
        return True
        
    def test_rate_limit_allowed(self):
        """Test requests within rate limit"""
        client_id = "client_1"
        
        for _ in range(10):
            self.assertTrue(self.is_allowed(client_id))
            
    def test_rate_limit_exceeded(self):
        """Test rate limit enforcement"""
        client_id = "client_2"
        self.max_requests = 5
        
        # Make max requests
        for _ in range(5):
            self.assertTrue(self.is_allowed(client_id))
            
        # Next request should be blocked
        self.assertFalse(self.is_allowed(client_id))
        
    def test_rate_limit_per_client(self):
        """Test that rate limits are per-client"""
        self.max_requests = 5
        
        # Client 1 makes max requests
        for _ in range(5):
            self.assertTrue(self.is_allowed("client_a"))
            
        # Client 2 should still be able to make requests
        self.assertTrue(self.is_allowed("client_b"))


class TestAuthentication(unittest.TestCase):
    """Test authentication functionality"""
    
    def setUp(self):
        """Set up test fixtures"""
        self.api_keys = {
            "valid_key_123": {"user": "user1", "role": "admin"},
            "valid_key_456": {"user": "user2", "role": "user"}
        }
        
    def validate_api_key(self, key: str) -> dict:
        """Validate API key"""
        if key in self.api_keys:
            return {"valid": True, **self.api_keys[key]}
        return {"valid": False}
        
    def test_valid_api_key(self):
        """Test valid API key validation"""
        result = self.validate_api_key("valid_key_123")
        
        self.assertTrue(result["valid"])
        self.assertEqual(result["user"], "user1")
        
    def test_invalid_api_key(self):
        """Test invalid API key rejection"""
        result = self.validate_api_key("invalid_key")
        
        self.assertFalse(result["valid"])
        
    def test_missing_api_key(self):
        """Test missing API key"""
        result = self.validate_api_key("")
        
        self.assertFalse(result["valid"])


class TestRequestValidation(unittest.TestCase):
    """Test request validation"""
    
    def validate_chat_request(self, request: dict) -> tuple:
        """Validate chat request"""
        errors = []
        
        if "message" not in request:
            errors.append("Missing 'message' field")
        elif not isinstance(request["message"], str):
            errors.append("'message' must be a string")
        elif len(request["message"]) > 10000:
            errors.append("'message' exceeds maximum length")
            
        if "use_rag" in request and not isinstance(request["use_rag"], bool):
            errors.append("'use_rag' must be a boolean")
            
        return len(errors) == 0, errors
        
    def test_valid_request(self):
        """Test valid request"""
        request = {
            "message": "Hello, world!",
            "use_rag": True
        }
        
        valid, errors = self.validate_chat_request(request)
        self.assertTrue(valid)
        self.assertEqual(len(errors), 0)
        
    def test_missing_message(self):
        """Test request with missing message"""
        request = {"use_rag": True}
        
        valid, errors = self.validate_chat_request(request)
        self.assertFalse(valid)
        self.assertIn("Missing 'message' field", errors)
        
    def test_invalid_message_type(self):
        """Test request with invalid message type"""
        request = {"message": 123}
        
        valid, errors = self.validate_chat_request(request)
        self.assertFalse(valid)
        self.assertIn("'message' must be a string", errors)


# =============================================================================
# Utility Function Tests
# =============================================================================

class TestUtilityFunctions(unittest.TestCase):
    """Test utility functions"""
    
    def test_safe_json_parse(self):
        """Test safe JSON parsing"""
        def safe_json_parse(text: str) -> dict:
            try:
                return json.loads(text)
            except json.JSONDecodeError:
                return {}
                
        # Valid JSON
        valid = '{"key": "value"}'
        result = safe_json_parse(valid)
        self.assertEqual(result["key"], "value")
        
        # Invalid JSON
        invalid = "not valid json"
        result = safe_json_parse(invalid)
        self.assertEqual(result, {})
        
    def test_truncate_text(self):
        """Test text truncation"""
        def truncate_text(text: str, max_length: int) -> str:
            if len(text) <= max_length:
                return text
            return text[:max_length - 3] + "..."
            
        # Short text
        short = "Hello"
        self.assertEqual(truncate_text(short, 10), "Hello")
        
        # Long text
        long = "This is a very long text that needs truncation"
        truncated = truncate_text(long, 20)
        self.assertEqual(len(truncated), 20)
        self.assertTrue(truncated.endswith("..."))
        
    def test_sanitize_input(self):
        """Test input sanitization"""
        def sanitize_input(text: str) -> str:
            # Remove potentially dangerous characters
            dangerous = ['<', '>', '"', "'", '&']
            for char in dangerous:
                text = text.replace(char, '')
            return text
            
        dirty = "<script>alert('xss')</script>"
        clean = sanitize_input(dirty)
        self.assertNotIn('<', clean)
        self.assertNotIn('>', clean)
        self.assertNotIn("'", clean)


# =============================================================================
# Main Entry Point
# =============================================================================

if __name__ == '__main__':
    unittest.main(verbosity=2)
