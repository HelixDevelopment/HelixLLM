#!/usr/bin/env python3
"""
HelixLLM API Test Suite
=======================
Python-based comprehensive test suite for HelixLLM OpenAI-compatible API.

Usage:
    python test_api.py
    python test_api.py --base-url http://localhost:8000
    python test_api.py --api-key your-api-key
"""

import argparse
import json
import sys
import time
from typing import Optional, Dict, Any

import httpx


class Colors:
    """Terminal colors"""
    RED = '\033[0;31m'
    GREEN = '\033[0;32m'
    YELLOW = '\033[1;33m'
    BLUE = '\033[0;34m'
    NC = '\033[0m'  # No Color


class APITester:
    """HelixLLM API Tester"""
    
    def __init__(self, base_url: str, api_key: Optional[str] = None, model: str = "helix-llm"):
        self.base_url = base_url.rstrip('/')
        self.api_key = api_key
        self.model = model
        self.headers = {"Content-Type": "application/json"}
        if api_key:
            self.headers["Authorization"] = f"Bearer {api_key}"
        
        self.tests_passed = 0
        self.tests_failed = 0
        self.client = httpx.Client(timeout=30.0)
    
    def print_header(self, text: str):
        print(f"\n{Colors.BLUE}{'='*50}{Colors.NC}")
        print(f"{Colors.BLUE}{text}{Colors.NC}")
        print(f"{Colors.BLUE}{'='*50}{Colors.NC}")
    
    def print_success(self, text: str):
        print(f"{Colors.GREEN}✓ {text}{Colors.NC}")
        self.tests_passed += 1
    
    def print_error(self, text: str):
        print(f"{Colors.RED}✗ {text}{Colors.NC}")
        self.tests_failed += 1
    
    def print_info(self, text: str):
        print(f"{Colors.YELLOW}ℹ {text}{Colors.NC}")
    
    def make_request(
        self,
        method: str,
        endpoint: str,
        data: Optional[Dict[str, Any]] = None
    ) -> Dict[str, Any]:
        """Make HTTP request to API"""
        url = f"{self.base_url}{endpoint}"
        
        try:
            if method == "GET":
                response = self.client.get(url, headers=self.headers)
            elif method == "POST":
                response = self.client.post(url, headers=self.headers, json=data)
            else:
                raise ValueError(f"Unsupported method: {method}")
            
            response.raise_for_status()
            return response.json()
        except httpx.HTTPStatusError as e:
            return {"error": f"HTTP {e.response.status_code}: {e.response.text}"}
        except Exception as e:
            return {"error": str(e)}
    
    def test_health(self) -> bool:
        """Test health endpoint"""
        self.print_header("Test 1: Health Check")
        response = self.make_request("GET", "/health")
        
        if response.get("status") == "healthy":
            self.print_success("Health check passed")
            print(f"  Response: {json.dumps(response, indent=2)}")
            return True
        else:
            self.print_error(f"Health check failed: {response}")
            return False
    
    def test_root(self) -> bool:
        """Test root endpoint"""
        self.print_header("Test 2: Root Endpoint")
        response = self.make_request("GET", "/")
        
        if "HelixLLM" in str(response):
            self.print_success("Root endpoint accessible")
            print(f"  Response: {json.dumps(response, indent=2)}")
            return True
        else:
            self.print_error(f"Root endpoint failed: {response}")
            return False
    
    def test_list_models(self) -> bool:
        """Test model listing"""
        self.print_header("Test 3: List Models")
        response = self.make_request("GET", "/v1/models")
        
        if response.get("object") == "list":
            self.print_success("Model listing works")
            models = response.get("data", [])
            print(f"  Available models: {len(models)}")
            for model in models:
                print(f"    - {model.get('id')}")
            return True
        else:
            self.print_error(f"Model listing failed: {response}")
            return False
    
    def test_get_model(self) -> bool:
        """Test getting specific model"""
        self.print_header("Test 4: Get Specific Model")
        response = self.make_request("GET", f"/v1/models/{self.model}")
        
        if response.get("id") == self.model:
            self.print_success("Model info retrieval works")
            print(f"  Model: {response.get('id')}")
            print(f"  Owned by: {response.get('owned_by')}")
            return True
        else:
            self.print_error(f"Model info retrieval failed: {response}")
            return False
    
    def test_chat_completion_simple(self) -> bool:
        """Test simple chat completion"""
        self.print_header("Test 5: Simple Chat Completion")
        response = self.make_request("POST", "/v1/chat/completions", {
            "model": self.model,
            "messages": [{"role": "user", "content": "Hello!"}]
        })
        
        if response.get("object") == "chat.completion":
            self.print_success("Chat completion works")
            content = response.get("choices", [{}])[0].get("message", {}).get("content")
            print(f"  Response: {content[:100]}..." if content and len(content) > 100 else f"  Response: {content}")
            usage = response.get("usage", {})
            print(f"  Tokens: {usage.get('total_tokens', 'N/A')} total")
            return True
        else:
            self.print_error(f"Chat completion failed: {response}")
            return False
    
    def test_chat_completion_with_system(self) -> bool:
        """Test chat completion with system message"""
        self.print_header("Test 6: Chat with System Message")
        response = self.make_request("POST", "/v1/chat/completions", {
            "model": self.model,
            "messages": [
                {"role": "system", "content": "You are a helpful assistant."},
                {"role": "user", "content": "What is your name?"}
            ]
        })
        
        if response.get("object") == "chat.completion":
            self.print_success("Chat with system message works")
            return True
        else:
            self.print_error(f"Chat with system message failed: {response}")
            return False
    
    def test_chat_completion_with_params(self) -> bool:
        """Test chat completion with parameters"""
        self.print_header("Test 7: Chat with Parameters")
        response = self.make_request("POST", "/v1/chat/completions", {
            "model": self.model,
            "messages": [{"role": "user", "content": "Say hello"}],
            "temperature": 0.5,
            "max_tokens": 100,
            "top_p": 0.9,
            "presence_penalty": 0.1,
            "frequency_penalty": 0.1
        })
        
        if response.get("object") == "chat.completion":
            self.print_success("Chat with parameters works")
            return True
        else:
            self.print_error(f"Chat with parameters failed: {response}")
            return False
    
    def test_streaming(self) -> bool:
        """Test streaming chat completion"""
        self.print_header("Test 8: Streaming Chat Completion")
        
        try:
            url = f"{self.base_url}/v1/chat/completions"
            data = {
                "model": self.model,
                "messages": [{"role": "user", "content": "Hi"}],
                "stream": True
            }
            
            chunks = []
            with self.client.stream("POST", url, headers=self.headers, json=data, timeout=30.0) as response:
                for line in response.iter_lines():
                    if line.startswith("data: "):
                        chunk = line[6:]  # Remove "data: " prefix
                        if chunk == "[DONE]":
                            break
                        try:
                            chunk_data = json.loads(chunk)
                            chunks.append(chunk_data)
                        except json.JSONDecodeError:
                            pass
            
            if chunks:
                self.print_success("Streaming works")
                print(f"  Chunks received: {len(chunks)}")
                return True
            else:
                self.print_error("No stream chunks received")
                return False
        except Exception as e:
            self.print_error(f"Streaming failed: {e}")
            return False
    
    def test_tool_calling(self) -> bool:
        """Test tool calling"""
        self.print_header("Test 9: Tool Calling")
        response = self.make_request("POST", "/v1/chat/completions", {
            "model": self.model,
            "messages": [{"role": "user", "content": "What is the weather in New York?"}],
            "tools": [{
                "type": "function",
                "function": {
                    "name": "get_weather",
                    "description": "Get weather information for a location",
                    "parameters": {
                        "type": "object",
                        "properties": {
                            "location": {"type": "string", "description": "City name"}
                        },
                        "required": ["location"]
                    }
                }
            }]
        })
        
        if response.get("object") == "chat.completion":
            self.print_success("Tool calling works")
            choices = response.get("choices", [{}])[0]
            if choices.get("finish_reason") == "tool_calls":
                tool_calls = choices.get("message", {}).get("tool_calls", [])
                print(f"  Tool calls: {len(tool_calls)}")
                for tc in tool_calls:
                    print(f"    - {tc.get('function', {}).get('name')}")
            return True
        else:
            self.print_error(f"Tool calling failed: {response}")
            return False
    
    def test_legacy_completions(self) -> bool:
        """Test legacy completions endpoint"""
        self.print_header("Test 10: Legacy Completions")
        response = self.make_request("POST", "/v1/completions", {
            "model": self.model,
            "prompt": "Once upon a time",
            "max_tokens": 50
        })
        
        if response.get("object") == "text_completion":
            self.print_success("Legacy completions work")
            text = response.get("choices", [{}])[0].get("text", "")
            print(f"  Generated: {text[:50]}..." if len(text) > 50 else f"  Generated: {text}")
            return True
        else:
            self.print_error(f"Legacy completions failed: {response}")
            return False
    
    def test_embeddings(self) -> bool:
        """Test embeddings endpoint"""
        self.print_header("Test 11: Embeddings")
        response = self.make_request("POST", "/v1/embeddings", {
            "model": self.model,
            "input": "Hello world"
        })
        
        if response.get("object") == "list":
            self.print_success("Embeddings work")
            data = response.get("data", [])
            if data:
                embedding = data[0].get("embedding", [])
                print(f"  Embedding dimensions: {len(embedding)}")
            return True
        else:
            self.print_error(f"Embeddings failed: {response}")
            return False
    
    def test_batch_embeddings(self) -> bool:
        """Test batch embeddings"""
        self.print_header("Test 12: Batch Embeddings")
        response = self.make_request("POST", "/v1/embeddings", {
            "model": self.model,
            "input": ["Hello world", "Goodbye world", "Test embedding"]
        })
        
        if response.get("object") == "list":
            self.print_success("Batch embeddings work")
            data = response.get("data", [])
            print(f"  Embeddings in batch: {len(data)}")
            return True
        else:
            self.print_error(f"Batch embeddings failed: {response}")
            return False
    
    def test_multi_turn_conversation(self) -> bool:
        """Test multi-turn conversation"""
        self.print_header("Test 13: Multi-turn Conversation")
        response = self.make_request("POST", "/v1/chat/completions", {
            "model": self.model,
            "messages": [
                {"role": "user", "content": "My name is Alice"},
                {"role": "assistant", "content": "Hello Alice! Nice to meet you."},
                {"role": "user", "content": "What is my name?"}
            ]
        })
        
        if response.get("object") == "chat.completion":
            self.print_success("Multi-turn conversation works")
            return True
        else:
            self.print_error(f"Multi-turn conversation failed: {response}")
            return False
    
    def test_error_handling(self) -> bool:
        """Test error handling"""
        self.print_header("Test 14: Error Handling")
        
        # Test missing required field
        response = self.make_request("POST", "/v1/chat/completions", {
            "model": self.model
            # Missing "messages"
        })
        
        if "error" in response:
            self.print_success("Error handling works correctly")
            return True
        else:
            self.print_error("Error not properly handled")
            return False
    
    def run_all_tests(self):
        """Run all tests"""
        print(f"{Colors.BLUE}")
        print("╔════════════════════════════════════════════════════════════╗")
        print("║         HelixLLM API Test Suite (Python)                   ║")
        print("╚════════════════════════════════════════════════════════════╝")
        print(f"{Colors.NC}")
        
        self.print_info(f"API Base URL: {self.base_url}")
        self.print_info(f"Model: {self.model}")
        self.print_info(f"Auth: {'Enabled' if self.api_key else 'Disabled'}")
        
        tests = [
            self.test_health,
            self.test_root,
            self.test_list_models,
            self.test_get_model,
            self.test_chat_completion_simple,
            self.test_chat_completion_with_system,
            self.test_chat_completion_with_params,
            self.test_streaming,
            self.test_tool_calling,
            self.test_legacy_completions,
            self.test_embeddings,
            self.test_batch_embeddings,
            self.test_multi_turn_conversation,
            self.test_error_handling,
        ]
        
        for test in tests:
            try:
                test()
            except Exception as e:
                self.print_error(f"Test crashed: {e}")
            time.sleep(0.1)  # Brief pause between tests
        
        # Summary
        print(f"\n{Colors.BLUE}{'='*50}{Colors.NC}")
        print(f"{Colors.BLUE}Test Summary{Colors.NC}")
        print(f"{Colors.BLUE}{'='*50}{Colors.NC}")
        print(f"{Colors.GREEN}Tests Passed: {self.tests_passed}{Colors.NC}")
        print(f"{Colors.RED}Tests Failed: {self.tests_failed}{Colors.NC}")
        
        if self.tests_failed == 0:
            print(f"\n{Colors.GREEN}All tests passed! ✓{Colors.NC}")
            return 0
        else:
            print(f"\n{Colors.RED}Some tests failed. Please check the output above.{Colors.NC}")
            return 1


def main():
    parser = argparse.ArgumentParser(description="HelixLLM API Test Suite")
    parser.add_argument("--base-url", default="http://localhost:8000", help="API base URL")
    parser.add_argument("--api-key", default=None, help="API key for authentication")
    parser.add_argument("--model", default="helix-llm", help="Model name")
    
    args = parser.parse_args()
    
    tester = APITester(
        base_url=args.base_url,
        api_key=args.api_key,
        model=args.model
    )
    
    sys.exit(tester.run_all_tests())


if __name__ == "__main__":
    main()
