#!/usr/bin/env python3
"""
HelixLLM API Client Example
===========================
Example Python client for interacting with HelixLLM OpenAI-compatible API.

This demonstrates how to use the API programmatically with the OpenAI client library
or direct HTTP requests.
"""

import os
from typing import Optional, List, Dict, Any

# Option 1: Using OpenAI Python client (recommended)
try:
    from openai import OpenAI
    HAS_OPENAI = True
except ImportError:
    HAS_OPENAI = False
    print("OpenAI client not installed. Install with: pip install openai")

# Option 2: Using httpx for direct HTTP requests
try:
    import httpx
    HAS_HTTPX = True
except ImportError:
    HAS_HTTPX = False


class HelixLLMClient:
    """
    Client for HelixLLM OpenAI-compatible API.
    
    Supports both OpenAI client library and direct HTTP requests.
    """
    
    def __init__(
        self,
        base_url: str = "http://localhost:8000/v1",
        api_key: Optional[str] = None,
        model: str = "helix-llm"
    ):
        self.base_url = base_url.rstrip('/')
        self.api_key = api_key or os.getenv("HELIXLLM_API_KEY", "")
        self.model = model
        
        # Initialize OpenAI client if available
        if HAS_OPENAI:
            self.client = OpenAI(
                base_url=self.base_url,
                api_key=self.api_key or "not-needed"
            )
        else:
            self.client = None
            if not HAS_HTTPX:
                raise ImportError("Either 'openai' or 'httpx' must be installed")
            self.http_client = httpx.Client(timeout=60.0)
    
    def chat_completion(
        self,
        messages: List[Dict[str, str]],
        temperature: float = 0.7,
        max_tokens: Optional[int] = None,
        stream: bool = False,
        tools: Optional[List[Dict]] = None,
        tool_choice: Optional[str] = "auto"
    ) -> Dict[str, Any]:
        """
        Create a chat completion.
        
        Args:
            messages: List of message dicts with 'role' and 'content'
            temperature: Sampling temperature (0-2)
            max_tokens: Maximum tokens to generate
            stream: Whether to stream the response
            tools: List of tool definitions
            tool_choice: Tool choice strategy ('auto', 'none', or specific tool)
        
        Returns:
            Completion response dict
        """
        if self.client:
            # Use OpenAI client
            kwargs = {
                "model": self.model,
                "messages": messages,
                "temperature": temperature,
                "stream": stream,
            }
            if max_tokens:
                kwargs["max_tokens"] = max_tokens
            if tools:
                kwargs["tools"] = tools
                kwargs["tool_choice"] = tool_choice
            
            response = self.client.chat.completions.create(**kwargs)
            
            if stream:
                return response  # Returns generator
            
            return {
                "id": response.id,
                "object": response.object,
                "created": response.created,
                "model": response.model,
                "choices": [
                    {
                        "index": choice.index,
                        "message": {
                            "role": choice.message.role,
                            "content": choice.message.content,
                            "tool_calls": [
                                {
                                    "id": tc.id,
                                    "type": tc.type,
                                    "function": {
                                        "name": tc.function.name,
                                        "arguments": tc.function.arguments
                                    }
                                } for tc in (choice.message.tool_calls or [])
                            ] if choice.message.tool_calls else None
                        },
                        "finish_reason": choice.finish_reason
                    }
                    for choice in response.choices
                ],
                "usage": {
                    "prompt_tokens": response.usage.prompt_tokens,
                    "completion_tokens": response.usage.completion_tokens,
                    "total_tokens": response.usage.total_tokens
                } if response.usage else None
            }
        else:
            # Use direct HTTP request
            url = f"{self.base_url}/chat/completions"
            headers = {"Content-Type": "application/json"}
            if self.api_key:
                headers["Authorization"] = f"Bearer {self.api_key}"
            
            data = {
                "model": self.model,
                "messages": messages,
                "temperature": temperature,
                "stream": stream
            }
            if max_tokens:
                data["max_tokens"] = max_tokens
            if tools:
                data["tools"] = tools
                data["tool_choice"] = tool_choice
            
            response = self.http_client.post(url, headers=headers, json=data)
            response.raise_for_status()
            return response.json()
    
    def stream_chat_completion(
        self,
        messages: List[Dict[str, str]],
        temperature: float = 0.7,
        max_tokens: Optional[int] = None
    ):
        """
        Stream chat completion chunks.
        
        Yields content chunks as they are generated.
        """
        if self.client:
            # Use OpenAI client streaming
            response = self.client.chat.completions.create(
                model=self.model,
                messages=messages,
                temperature=temperature,
                max_tokens=max_tokens,
                stream=True
            )
            
            for chunk in response:
                if chunk.choices:
                    delta = chunk.choices[0].delta
                    if delta.content:
                        yield delta.content
        else:
            # Use direct HTTP streaming
            url = f"{self.base_url}/chat/completions"
            headers = {"Content-Type": "application/json"}
            if self.api_key:
                headers["Authorization"] = f"Bearer {self.api_key}"
            
            data = {
                "model": self.model,
                "messages": messages,
                "temperature": temperature,
                "stream": True
            }
            if max_tokens:
                data["max_tokens"] = max_tokens
            
            with self.http_client.stream("POST", url, headers=headers, json=data) as response:
                for line in response.iter_lines():
                    if line.startswith("data: "):
                        chunk = line[6:]
                        if chunk == "[DONE]":
                            break
                        try:
                            import json
                            chunk_data = json.loads(chunk)
                            delta = chunk_data.get("choices", [{}])[0].get("delta", {})
                            if delta.get("content"):
                                yield delta["content"]
                        except json.JSONDecodeError:
                            pass
    
    def create_embedding(
        self,
        input_text: str,
        dimensions: Optional[int] = None
    ) -> List[float]:
        """
        Create embedding for input text.
        
        Args:
            input_text: Text to embed
            dimensions: Optional embedding dimensions
        
        Returns:
            Embedding vector
        """
        if self.client:
            response = self.client.embeddings.create(
                model=self.model,
                input=input_text,
                dimensions=dimensions
            )
            return response.data[0].embedding
        else:
            url = f"{self.base_url}/embeddings"
            headers = {"Content-Type": "application/json"}
            if self.api_key:
                headers["Authorization"] = f"Bearer {self.api_key}"
            
            data = {
                "model": self.model,
                "input": input_text
            }
            if dimensions:
                data["dimensions"] = dimensions
            
            response = self.http_client.post(url, headers=headers, json=data)
            response.raise_for_status()
            return response.json()["data"][0]["embedding"]
    
    def list_models(self) -> List[Dict[str, Any]]:
        """List available models."""
        if self.client:
            response = self.client.models.list()
            return [{"id": m.id, "object": m.object} for m in response.data]
        else:
            url = f"{self.base_url}/models"
            headers = {}
            if self.api_key:
                headers["Authorization"] = f"Bearer {self.api_key}"
            
            response = self.http_client.get(url, headers=headers)
            response.raise_for_status()
            return response.json().get("data", [])
    
    def close(self):
        """Close the client connection."""
        if self.client:
            self.client.close()
        elif hasattr(self, 'http_client'):
            self.http_client.close()


# =============================================================================
# EXAMPLE USAGE
# =============================================================================

def example_simple_chat():
    """Example: Simple chat completion"""
    print("=" * 50)
    print("Example: Simple Chat Completion")
    print("=" * 50)
    
    client = HelixLLMClient()
    
    response = client.chat_completion(
        messages=[{"role": "user", "content": "Hello! What can you do?"}]
    )
    
    print(f"Response ID: {response['id']}")
    print(f"Model: {response['model']}")
    print(f"Content: {response['choices'][0]['message']['content']}")
    print(f"Tokens used: {response['usage']['total_tokens']}")
    
    client.close()


def example_chat_with_system():
    """Example: Chat with system message"""
    print("\n" + "=" * 50)
    print("Example: Chat with System Message")
    print("=" * 50)
    
    client = HelixLLMClient()
    
    response = client.chat_completion(
        messages=[
            {"role": "system", "content": "You are a Python expert. Be concise."},
            {"role": "user", "content": "How do I read a file in Python?"}
        ],
        temperature=0.3
    )
    
    print(f"Content: {response['choices'][0]['message']['content']}")
    
    client.close()


def example_streaming():
    """Example: Streaming chat completion"""
    print("\n" + "=" * 50)
    print("Example: Streaming Chat Completion")
    print("=" * 50)
    
    client = HelixLLMClient()
    
    print("Response: ", end="", flush=True)
    for chunk in client.stream_chat_completion(
        messages=[{"role": "user", "content": "Tell me a short joke"}]
    ):
        print(chunk, end="", flush=True)
    print()
    
    client.close()


def example_tool_calling():
    """Example: Tool calling"""
    print("\n" + "=" * 50)
    print("Example: Tool Calling")
    print("=" * 50)
    
    client = HelixLLMClient()
    
    tools = [
        {
            "type": "function",
            "function": {
                "name": "get_weather",
                "description": "Get weather information for a location",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "location": {
                            "type": "string",
                            "description": "City name, e.g., 'New York'"
                        }
                    },
                    "required": ["location"]
                }
            }
        },
        {
            "type": "function",
            "function": {
                "name": "calculate",
                "description": "Perform a calculation",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "expression": {
                            "type": "string",
                            "description": "Mathematical expression"
                        }
                    },
                    "required": ["expression"]
                }
            }
        }
    ]
    
    response = client.chat_completion(
        messages=[{"role": "user", "content": "What's the weather in Paris?"}],
        tools=tools,
        tool_choice="auto"
    )
    
    message = response['choices'][0]['message']
    
    if message.get('tool_calls'):
        print("Tool calls detected:")
        for tool_call in message['tool_calls']:
            print(f"  - Function: {tool_call['function']['name']}")
            print(f"    Arguments: {tool_call['function']['arguments']}")
    else:
        print(f"Response: {message.get('content')}")
    
    client.close()


def example_embeddings():
    """Example: Creating embeddings"""
    print("\n" + "=" * 50)
    print("Example: Creating Embeddings")
    print("=" * 50)
    
    client = HelixLLMClient()
    
    embedding = client.create_embedding("Hello, world!")
    
    print(f"Embedding dimensions: {len(embedding)}")
    print(f"First 5 values: {embedding[:5]}")
    
    client.close()


def example_list_models():
    """Example: List available models"""
    print("\n" + "=" * 50)
    print("Example: List Models")
    print("=" * 50)
    
    client = HelixLLMClient()
    
    models = client.list_models()
    
    print("Available models:")
    for model in models:
        print(f"  - {model['id']}")
    
    client.close()


def example_conversation():
    """Example: Multi-turn conversation"""
    print("\n" + "=" * 50)
    print("Example: Multi-turn Conversation")
    print("=" * 50)
    
    client = HelixLLMClient()
    
    # Conversation history
    messages = [
        {"role": "user", "content": "My name is Alice"},
    ]
    
    response = client.chat_completion(messages=messages)
    assistant_reply = response['choices'][0]['message']['content']
    print(f"User: My name is Alice")
    print(f"Assistant: {assistant_reply}")
    
    # Add to history and continue
    messages.append({"role": "assistant", "content": assistant_reply})
    messages.append({"role": "user", "content": "What's my name?"})
    
    response = client.chat_completion(messages=messages)
    print(f"User: What's my name?")
    print(f"Assistant: {response['choices'][0]['message']['content']}")
    
    client.close()


# =============================================================================
# MAIN
# =============================================================================

if __name__ == "__main__":
    import sys
    
    # Check if server is running
    try:
        import httpx
        response = httpx.get("http://localhost:8000/health", timeout=5.0)
        if response.json().get("status") != "healthy":
            print("HelixLLM API server is not healthy")
            sys.exit(1)
    except Exception as e:
        print(f"Cannot connect to HelixLLM API server: {e}")
        print("Please start the server first with: python main.py")
        sys.exit(1)
    
    print("\n" + "=" * 50)
    print("HelixLLM API Client Examples")
    print("=" * 50)
    
    # Run all examples
    try:
        example_simple_chat()
        example_chat_with_system()
        example_streaming()
        example_tool_calling()
        example_embeddings()
        example_list_models()
        example_conversation()
        
        print("\n" + "=" * 50)
        print("All examples completed successfully!")
        print("=" * 50)
        
    except Exception as e:
        print(f"\nError running examples: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)
