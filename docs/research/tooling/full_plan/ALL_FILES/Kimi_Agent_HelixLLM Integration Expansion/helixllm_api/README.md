# HelixLLM OpenAI-Compatible API Server

A complete FastAPI implementation providing OpenAI API compatibility for CLI agents like OpenCode, Crush, Gemini CLI, and Claude Code.

## Features

- ✅ **Full OpenAI API Compatibility** - Drop-in replacement for OpenAI endpoints
- ✅ **Streaming Responses** - Server-Sent Events (SSE) support
- ✅ **Tool/Function Calling** - Full tool definition and execution support
- ✅ **Multiple Endpoints** - Chat completions, legacy completions, embeddings, models
- ✅ **Authentication** - Optional API key validation
- ✅ **Rate Limiting** - Configurable request throttling
- ✅ **CORS Support** - Cross-origin resource sharing
- ✅ **Comprehensive Error Handling** - OpenAI-compatible error responses
- ✅ **Docker Deployment** - Ready-to-use Docker and docker-compose configurations

## Quick Start

### 1. Installation

```bash
# Clone or download the HelixLLM API
cd helixllm_api

# Install dependencies
pip install -r requirements.txt
```

### 2. Configuration

```bash
# Copy example environment file
cp .env.example .env

# Edit .env with your settings
nano .env
```

### 3. Start the Server

```bash
# Start the API server
python main.py

# Or with uvicorn directly
uvicorn main:app --host 0.0.0.0 --port 8000 --reload
```

### 4. Test the API

```bash
# Run test suite
./test_api.sh

# Or with Python
python test_api.py
```

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Server info and available endpoints |
| `/health` | GET | Health check |
| `/v1/models` | GET | List available models |
| `/v1/models/{model_id}` | GET | Get model information |
| `/v1/chat/completions` | POST | Create chat completion |
| `/v1/completions` | POST | Create completion (legacy) |
| `/v1/embeddings` | POST | Create embeddings |

## Configuration Options

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIXLLM_HOST` | `0.0.0.0` | Server bind address |
| `HELIXLLM_PORT` | `8000` | Server port |
| `HELIXLLM_API_KEY` | (empty) | API key for authentication |
| `HELIXLLM_MODEL` | `helix-llm` | Model identifier |
| `HELIXLLM_VERSION` | `1.0.0` | API version |
| `HELIXLLM_MAX_TOKENS` | `4096` | Maximum tokens per request |
| `HELIXLLM_TEMPERATURE` | `0.7` | Default temperature |
| `HELIXLLM_ENABLE_STREAMING` | `true` | Enable streaming responses |
| `HELIXLLM_LOG_LEVEL` | `INFO` | Logging level |
| `HELIXLLM_CORS_ORIGINS` | `*` | Allowed CORS origins |
| `HELIXLLM_RATE_LIMIT` | `false` | Enable rate limiting |
| `HELIXLLM_RATE_LIMIT_REQUESTS` | `100` | Max requests per window |
| `HELIXLLM_RATE_LIMIT_WINDOW` | `60` | Rate limit window (seconds) |

## CLI Agent Configuration

### OpenCode

```bash
# Environment variables
export OPENCODE_API_KEY=your-api-key
export OPENCODE_API_BASE_URL=http://localhost:8000/v1
export OPENCODE_MODEL=helix-llm

# Or config file ~/.opencode/config.json
{
  "api_provider": "openai",
  "api_key": "your-api-key",
  "api_base_url": "http://localhost:8000/v1",
  "model": "helix-llm"
}
```

### Crush

```bash
# Environment variables
export CRUSH_API_KEY=your-api-key
export CRUSH_API_BASE_URL=http://localhost:8000/v1
export CRUSH_MODEL=helix-llm
```

### Gemini CLI

```bash
# Environment variables
export GEMINI_API_KEY=your-api-key
export GEMINI_API_BASE_URL=http://localhost:8000/v1
export GEMINI_MODEL=helix-llm
```

### Claude Code

```bash
# Environment variables
export ANTHROPIC_BASE_URL=http://localhost:8000/v1
export ANTHROPIC_API_KEY=your-api-key
export CLAUDE_CODE_MODEL=helix-llm
```

See [CLI_AGENT_CONFIGURATION.md](CLI_AGENT_CONFIGURATION.md) for detailed configuration instructions.

## Docker Deployment

### Using Docker

```bash
# Build the image
docker build -t helixllm-api .

# Run the container
docker run -d \
  -p 8000:8000 \
  -e HELIXLLM_API_KEY=your-api-key \
  -e HELIXLLM_MODEL=helix-llm \
  helixllm-api
```

### Using Docker Compose

```bash
# Start the services
docker-compose up -d

# With optional services
docker-compose --profile with-nginx up -d
docker-compose --profile monitoring up -d

# View logs
docker-compose logs -f helixllm-api

# Stop services
docker-compose down
```

## API Examples

### Chat Completion

```bash
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "helix-llm",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "Hello!"}
    ],
    "temperature": 0.7,
    "max_tokens": 150
  }'
```

### Streaming Chat Completion

```bash
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "helix-llm",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'
```

### Tool Calling

```bash
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "helix-llm",
    "messages": [{"role": "user", "content": "What is the weather in New York?"}],
    "tools": [{
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get weather information",
        "parameters": {
          "type": "object",
          "properties": {
            "location": {"type": "string"}
          }
        }
      }
    }]
  }'
```

### Embeddings

```bash
curl -X POST http://localhost:8000/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "model": "helix-llm",
    "input": "Hello world"
  }'
```

## Request/Response Schemas

### Chat Completion Request

```json
{
  "model": "helix-llm",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello!"}
  ],
  "temperature": 0.7,
  "max_tokens": 150,
  "stream": false,
  "tools": null,
  "tool_choice": "auto"
}
```

### Chat Completion Response

```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "created": 1234567890,
  "model": "helix-llm",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "Hello! How can I help you today?"
    },
    "finish_reason": "stop"
  }],
  "usage": {
    "prompt_tokens": 20,
    "completion_tokens": 10,
    "total_tokens": 30
  }
}
```

### Streaming Chunk

```
data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1234567890,"model":"helix-llm","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1234567890,"model":"helix-llm","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1234567890,"model":"helix-llm","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1234567890,"model":"helix-llm","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

## Integrating with Actual HelixLLM Backend

The current implementation includes a mock backend for testing. To integrate with your actual HelixLLM model:

1. **Modify `HelixLLMBackend` class** in `main.py`:

```python
class HelixLLMBackend:
    async def chat_completion(self, messages, **kwargs):
        # Replace with actual HelixLLM integration
        # Example:
        response = await your_helixllm_model.generate(
            messages=messages,
            temperature=kwargs.get('temperature', 0.7),
            max_tokens=kwargs.get('max_tokens'),
            tools=kwargs.get('tools')
        )
        return {
            "content": response.text,
            "tool_calls": response.tool_calls,
            "finish_reason": response.finish_reason,
            "usage": {
                "prompt_tokens": response.prompt_tokens,
                "completion_tokens": response.completion_tokens,
                "total_tokens": response.total_tokens
            }
        }
```

2. **Implement streaming**:

```python
async def stream_chat_completion(self, messages, **kwargs):
    async for chunk in your_helixllm_model.generate_stream(messages, **kwargs):
        yield chunk.text
```

3. **Implement embeddings**:

```python
async def create_embedding(self, input_text, model, dimensions=None):
    embeddings = await your_helixllm_model.embed(input_text)
    return embeddings
```

## Testing

### Run All Tests

```bash
# Bash test suite
chmod +x test_api.sh
./test_api.sh

# Python test suite
python test_api.py

# With custom settings
python test_api.py --base-url http://localhost:8000 --api-key your-key
```

### Manual Testing

```bash
# Health check
curl http://localhost:8000/health

# List models
curl http://localhost:8000/v1/models

# Simple chat
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"helix-llm","messages":[{"role":"user","content":"Hello"}]}'
```

## Troubleshooting

### Connection Refused

```bash
# Check if server is running
curl http://localhost:8000/health

# Check port availability
lsof -i :8000
```

### Authentication Errors

```bash
# If using API key, verify it's set correctly
echo $HELIXLLM_API_KEY

# Test with explicit key
curl -H "Authorization: Bearer your-key" http://localhost:8000/v1/models
```

### Model Not Found

CLI agents may request models by different names. The server accepts any model name but logs a warning. To handle specific model names:

```python
# In main.py, modify model validation
SUPPORTED_MODELS = ["helix-llm", "gpt-4", "gpt-3.5-turbo"]

if request.model not in SUPPORTED_MODELS:
    # Map to your model or return error
    pass
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    CLI Agents                                │
│  (OpenCode, Crush, Gemini CLI, Claude Code, etc.)          │
└───────────────────────┬─────────────────────────────────────┘
                        │ OpenAI-compatible API calls
                        ▼
┌─────────────────────────────────────────────────────────────┐
│                 HelixLLM API Server                          │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  FastAPI Application                                  │  │
│  │  ├── CORS Middleware                                  │  │
│  │  ├── Authentication Middleware                        │  │
│  │  ├── Rate Limiting Middleware                         │  │
│  │  └── Error Handling                                   │  │
│  └───────────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  API Endpoints                                        │  │
│  │  ├── GET  /v1/models                                  │  │
│  │  ├── POST /v1/chat/completions                        │  │
│  │  ├── POST /v1/completions                             │  │
│  │  └── POST /v1/embeddings                              │  │
│  └───────────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  HelixLLM Backend                                     │  │
│  │  ├── Chat Completion                                  │  │
│  │  ├── Streaming Generation                             │  │
│  │  ├── Tool Calling                                     │  │
│  │  └── Embeddings                                       │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests
5. Submit a pull request

## License

MIT License - See LICENSE file for details

## Support

For issues and questions:
- Check the [troubleshooting section](#troubleshooting)
- Review [CLI agent configuration](CLI_AGENT_CONFIGURATION.md)
- Open an issue on GitHub
