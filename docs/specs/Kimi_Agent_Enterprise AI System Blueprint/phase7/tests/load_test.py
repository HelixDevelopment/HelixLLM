#!/usr/bin/env python3
"""
=============================================================================
Light Local LLM System - Load Testing Suite
=============================================================================
Load testing using locust for:
- API endpoint stress testing
- Concurrent user simulation
- Performance under load
- Bottleneck identification

Usage:
    locust -f load_test.py --host=http://localhost:8080
    locust -f load_test.py --host=http://localhost:8080 -u 100 -r 10
=============================================================================
"""

import random
import json
from locust import HttpUser, task, between, events
from locust.runners import MasterRunner


# =============================================================================
# Test Data
# =============================================================================

TEST_QUERIES = [
    "What is machine learning?",
    "Explain neural networks",
    "How does backpropagation work?",
    "What are transformers in AI?",
    "Describe natural language processing",
    "What is reinforcement learning?",
    "Explain computer vision",
    "What are GANs?",
    "How does sentiment analysis work?",
    "What is transfer learning?",
    "Explain deep learning",
    "What is supervised learning?",
    "Describe unsupervised learning",
    "What is the attention mechanism?",
    "Explain model fine-tuning"
]

CHAT_MESSAGES = [
    "Hello, how are you?",
    "Tell me a joke",
    "What can you help me with?",
    "Explain quantum computing",
    "Write a Python function",
    "Summarize the concept of AI",
    "What is the weather like?",
    "Help me understand RAG",
    "Explain vector databases",
    "What is embedding?"
]


# =============================================================================
# Custom Event Handlers
# =============================================================================

@events.request.add_listener
def on_request(request_type, name, response_time, response_length, 
               response, context, exception, **kwargs):
    """Log slow requests"""
    if response_time > 5000:  # Log requests over 5 seconds
        print(f"SLOW REQUEST: {name} took {response_time}ms")


@events.test_stop.add_listener
def on_test_stop(environment, **kwargs):
    """Print summary when test stops"""
    print("\n" + "="*60)
    print("LOAD TEST COMPLETED")
    print("="*60)


# =============================================================================
# Base User Class
# =============================================================================

class LLMUser(HttpUser):
    """Base user class for LLM system load testing"""
    abstract = True
    
    def on_start(self):
        """Called when a user starts"""
        self.client.headers.update({
            "Content-Type": "application/json",
            "Accept": "application/json"
        })
        
    def get_random_query(self) -> str:
        """Get a random test query"""
        return random.choice(TEST_QUERIES)
        
    def get_random_message(self) -> str:
        """Get a random chat message"""
        return random.choice(CHAT_MESSAGES)


# =============================================================================
# API Gateway Load Tests
# =============================================================================

class APIGatewayUser(LLMUser):
    """Simulate users interacting with API Gateway"""
    wait_time = between(1, 5)
    weight = 3
    
    @task(1)
    def health_check(self):
        """Test health endpoint"""
        with self.client.get("/health", catch_response=True) as response:
            if response.status_code == 200:
                data = response.json()
                if data.get("status") == "healthy":
                    response.success()
                else:
                    response.failure("Unhealthy status")
                    
    @task(5)
    def chat_without_rag(self):
        """Test chat endpoint without RAG"""
        payload = {
            "message": self.get_random_message(),
            "use_rag": False,
            "stream": False
        }
        
        with self.client.post("/chat", json=payload, 
                             timeout=120, catch_response=True) as response:
            if response.status_code == 200:
                data = response.json()
                if "response" in data:
                    response.success()
                else:
                    response.failure("Missing response field")
            elif response.status_code == 429:
                response.success()  # Rate limiting is expected
            else:
                response.failure(f"Status: {response.status_code}")
                
    @task(3)
    def chat_with_rag(self):
        """Test chat endpoint with RAG"""
        payload = {
            "message": self.get_random_query(),
            "use_rag": True,
            "stream": False
        }
        
        with self.client.post("/chat", json=payload, 
                             timeout=180, catch_response=True) as response:
            if response.status_code == 200:
                data = response.json()
                if "response" in data:
                    response.success()
                else:
                    response.failure("Missing response field")
            elif response.status_code == 429:
                response.success()
            else:
                response.failure(f"Status: {response.status_code}")
                
    @task(2)
    def stream_chat(self):
        """Test streaming chat endpoint"""
        payload = {
            "message": self.get_random_message(),
            "use_rag": False,
            "stream": True
        }
        
        with self.client.post("/chat", json=payload, 
                             timeout=120, catch_response=True,
                             stream=True) as response:
            if response.status_code == 200:
                # Read streamed response
                chunks = 0
                for chunk in response.iter_content(chunk_size=1024):
                    if chunk:
                        chunks += 1
                if chunks > 0:
                    response.success()
                else:
                    response.failure("Empty stream")
            elif response.status_code == 429:
                response.success()
            else:
                response.failure(f"Status: {response.status_code}")


# =============================================================================
# RAG Service Load Tests
# =============================================================================

class RAGServiceUser(LLMUser):
    """Simulate users interacting with RAG service"""
    wait_time = between(2, 8)
    weight = 2
    
    @task(1)
    def health_check(self):
        """Test RAG health endpoint"""
        self.client.get("/health")
        
    @task(8)
    def query_documents(self):
        """Test document query"""
        payload = {
            "query": self.get_random_query(),
            "top_k": random.randint(3, 10),
            "include_sources": True
        }
        
        with self.client.post("/query", json=payload, 
                             timeout=60, catch_response=True) as response:
            if response.status_code == 200:
                data = response.json()
                if "response" in data:
                    response.success()
                else:
                    response.failure("Missing response")
            else:
                response.failure(f"Status: {response.status_code}")
                
    @task(2)
    def index_document(self):
        """Test document indexing"""
        payload = {
            "content": f"Test document content {random.randint(1, 1000)}",
            "metadata": {
                "source": "load_test",
                "type": "test",
                "timestamp": random.randint(1, 1000000)
            }
        }
        
        with self.client.post("/index", json=payload, 
                             timeout=30, catch_response=True) as response:
            if response.status_code == 200:
                response.success()
            else:
                response.failure(f"Status: {response.status_code}")


# =============================================================================
# MCP Server Load Tests
# =============================================================================

class MCPServerUser(LLMUser):
    """Simulate users interacting with MCP server"""
    wait_time = between(1, 3)
    weight = 1
    
    @task(1)
    def health_check(self):
        """Test MCP health endpoint"""
        self.client.get("/health")
        
    @task(5)
    def list_tools(self):
        """Test tool listing"""
        with self.client.get("/tools", catch_response=True) as response:
            if response.status_code == 200:
                response.success()
            else:
                response.failure(f"Status: {response.status_code}")
                
    @task(3)
    def call_random_tool(self):
        """Test calling a random tool"""
        # First get available tools
        response = self.client.get("/tools")
        if response.status_code != 200:
            return
            
        tools = response.json().get("tools", [])
        if not tools:
            return
            
        tool = random.choice(tools)
        tool_name = tool["name"]
        
        # Call the tool with sample input
        payload = {"input": f"test input {random.randint(1, 1000)}"}
        
        with self.client.post(f"/tools/{tool_name}", json=payload,
                             timeout=30, catch_response=True) as response:
            # Accept 200, 400 (validation error), or 422
            if response.status_code in [200, 400, 422]:
                response.success()
            else:
                response.failure(f"Status: {response.status_code}")


# =============================================================================
# Ollama Load Tests
# =============================================================================

class OllamaUser(HttpUser):
    """Simulate users interacting directly with Ollama"""
    host = "http://localhost:11434"
    wait_time = between(3, 10)
    weight = 1
    
    @task(1)
    def list_models(self):
        """Test model listing"""
        self.client.get("/api/tags")
        
    @task(5)
    def generate_completion(self):
        """Test text generation"""
        payload = {
            "model": "llama3.2",
            "prompt": random.choice(CHAT_MESSAGES),
            "stream": False,
            "options": {
                "temperature": 0.7,
                "num_predict": 128
            }
        }
        
        with self.client.post("/api/generate", json=payload,
                             timeout=120, catch_response=True) as response:
            if response.status_code == 200:
                response.success()
            else:
                response.failure(f"Status: {response.status_code}")
                
    @task(3)
    def chat_completion(self):
        """Test chat completion"""
        payload = {
            "model": "llama3.2",
            "messages": [
                {"role": "user", "content": random.choice(CHAT_MESSAGES)}
            ],
            "stream": False
        }
        
        with self.client.post("/api/chat", json=payload,
                             timeout=120, catch_response=True) as response:
            if response.status_code == 200:
                response.success()
            else:
                response.failure(f"Status: {response.status_code}")


# =============================================================================
# Stress Test Configuration
# =============================================================================

class StressTestUser(LLMUser):
    """High-load stress test user"""
    wait_time = between(0.1, 0.5)
    weight = 0  # Disabled by default, enable for stress testing
    
    @task(10)
    def rapid_health_checks(self):
        """Rapid health check requests"""
        self.client.get("/health")
        
    @task(5)
    def rapid_chat(self):
        """Rapid chat requests"""
        payload = {
            "message": "Quick test",
            "use_rag": False,
            "stream": False
        }
        self.client.post("/chat", json=payload, timeout=30)


# =============================================================================
# Spike Test Configuration
# =============================================================================

class SpikeTestUser(LLMUser):
    """Spike test user - sudden burst of traffic"""
    wait_time = between(0, 0.1)
    weight = 0  # Enable only for spike testing
    
    @task(1)
    def burst_request(self):
        """Generate burst traffic"""
        payload = {
            "message": random.choice(CHAT_MESSAGES),
            "use_rag": False,
            "stream": False
        }
        self.client.post("/chat", json=payload, timeout=30)


# =============================================================================
# Soak Test Configuration
# =============================================================================

class SoakTestUser(LLMUser):
    """Soak test user - sustained load over time"""
    wait_time = between(5, 15)
    weight = 0  # Enable only for soak testing
    
    @task(3)
    def normal_chat(self):
        """Normal chat pattern"""
        payload = {
            "message": self.get_random_message(),
            "use_rag": random.choice([True, False]),
            "stream": False
        }
        self.client.post("/chat", json=payload, timeout=180)
        
    @task(1)
    def rag_query(self):
        """RAG query pattern"""
        payload = {
            "query": self.get_random_query(),
            "top_k": 5
        }
        self.client.post("/query", json=payload, timeout=60)


# =============================================================================
# Custom Commands
# =============================================================================

@events.init_command_line_parser.add_listener
def add_custom_arguments(parser):
    """Add custom command line arguments"""
    parser.add_argument(
        "--test-type",
        choices=["load", "stress", "spike", "soak"],
        default="load",
        help="Type of test to run"
    )
    parser.add_argument(
        "--rag-ratio",
        type=float,
        default=0.3,
        help="Ratio of RAG vs non-RAG requests (0-1)"
    )


# =============================================================================
# Main Entry Point
# =============================================================================

if __name__ == "__main__":
    import sys
    print("Light Local LLM System - Load Testing Suite")
    print("="*60)
    print("\nUsage:")
    print("  locust -f load_test.py --host=http://localhost:8080")
    print("  locust -f load_test.py --host=http://localhost:8080 -u 100 -r 10")
    print("\nTest Types:")
    print("  - Load Test: Steady traffic to measure normal performance")
    print("  - Stress Test: Increasing load to find breaking point")
    print("  - Spike Test: Sudden traffic bursts")
    print("  - Soak Test: Sustained load over extended period")
