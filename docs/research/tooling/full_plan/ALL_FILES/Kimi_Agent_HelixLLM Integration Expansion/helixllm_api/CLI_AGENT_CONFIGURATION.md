# CLI Agent Configuration Guide for HelixLLM

This guide provides detailed instructions for configuring popular CLI agents to use HelixLLM as a drop-in replacement for OpenAI models.

## Table of Contents

1. [OpenCode Configuration](#opencode)
2. [Crush Configuration](#crush)
3. [Gemini CLI Configuration](#gemini-cli)
4. [Claude Code Configuration](#claude-code)
5. [Environment Variables](#environment-variables)
6. [Testing Your Setup](#testing)

---

## <a name="opencode"></a>1. OpenCode Configuration

OpenCode is an AI-powered coding assistant that supports custom OpenAI-compatible endpoints.

### Installation

```bash
# Install OpenCode globally
npm install -g opencode

# Or use npx
npx opencode
```

### Configuration File

Create or edit `~/.opencode/config.json`:

```json
{
  "api_provider": "openai",
  "api_key": "your-helixllm-api-key-or-any-value",
  "api_base_url": "http://localhost:8000/v1",
  "model": "helix-llm",
  "temperature": 0.7,
  "max_tokens": 4096,
  "stream": true,
  "tools_enabled": true
}
```

### Environment Variables

```bash
# Add to your ~/.bashrc, ~/.zshrc, or shell profile
export OPENCODE_API_PROVIDER=openai
export OPENCODE_API_KEY=your-helixllm-api-key-or-any-value
export OPENCODE_API_BASE_URL=http://localhost:8000/v1
export OPENCODE_MODEL=helix-llm
```

### Project-Specific Configuration

Create `.opencode.json` in your project root:

```json
{
  "api_provider": "openai",
  "api_key": "${HELIXLLM_API_KEY}",
  "api_base_url": "${HELIXLLM_BASE_URL}",
  "model": "helix-llm",
  "system_prompt": "You are a helpful coding assistant powered by HelixLLM.",
  "file_context": {
    "include": ["src/**/*", "lib/**/*"],
    "exclude": ["node_modules/**/*", "dist/**/*", ".git/**/*"]
  }
}
```

### Usage

```bash
# Start interactive session
opencode

# Run with specific prompt
opencode "Explain this codebase"

# With file context
opencode -f src/main.py "Review this file"

# With tools enabled
opencode --tools "Create a React component"
```

---

## <a name="crush"></a>2. Crush Configuration

Crush is a CLI tool for AI-powered code reviews and assistance.

### Installation

```bash
# Install Crush CLI
npm install -g crush-cli

# Or via yarn
yarn global add crush-cli
```

### Configuration File

Create `~/.crush/config.yaml`:

```yaml
# Crush CLI Configuration for HelixLLM
api:
  provider: openai
  base_url: http://localhost:8000/v1
  api_key: your-helixllm-api-key-or-any-value
  
model:
  name: helix-llm
  temperature: 0.7
  max_tokens: 4096
  
features:
  streaming: true
  tools: true
  context_aware: true
  
review:
  auto_approve: false
  show_diff: true
  
defaults:
  context_lines: 10
  max_file_size: 100000
```

### Environment Variables

```bash
# Crush environment configuration
export CRUSH_API_PROVIDER=openai
export CRUSH_API_BASE_URL=http://localhost:8000/v1
export CRUSH_API_KEY=your-helixllm-api-key-or-any-value
export CRUSH_MODEL=helix-llm
```

### Project Configuration

Create `.crushrc` in project root:

```ini
[api]
provider = openai
base_url = http://localhost:8000/v1
api_key = your-helixllm-api-key

[model]
name = helix-llm
temperature = 0.7
max_tokens = 4096

[review]
patterns = *.py,*.js,*.ts,*.jsx,*.tsx
exclude = node_modules/,dist/,build/
```

### Usage

```bash
# Review current changes
crush review

# Review specific file
crush review src/main.py

# Interactive mode
crush interactive

# With custom prompt
crush ask "How can I improve this function?"
```

---

## <a name="gemini-cli"></a>3. Gemini CLI Configuration

Gemini CLI can be configured to use OpenAI-compatible endpoints.

### Installation

```bash
# Install Gemini CLI
npm install -g @google/gemini-cli

# Or download from GitHub releases
# https://github.com/google-gemini/gemini-cli/releases
```

### Configuration File

Create `~/.gemini/config.json`:

```json
{
  "provider": "openai-compatible",
  "api": {
    "baseUrl": "http://localhost:8000/v1",
    "apiKey": "your-helixllm-api-key-or-any-value",
    "model": "helix-llm"
  },
  "generation": {
    "temperature": 0.7,
    "maxOutputTokens": 4096,
    "topP": 0.95,
    "topK": 40
  },
  "features": {
    "streaming": true,
    "tools": true,
    "grounding": false
  },
  "safetySettings": [
    {
      "category": "HARM_CATEGORY_DANGEROUS_CONTENT",
      "threshold": "BLOCK_NONE"
    }
  ]
}
```

### Environment Variables

```bash
# Gemini CLI environment configuration
export GEMINI_API_PROVIDER=openai-compatible
export GEMINI_API_BASE_URL=http://localhost:8000/v1
export GEMINI_API_KEY=your-helixllm-api-key-or-any-value
export GEMINI_MODEL=helix-llm
```

### Shell Integration

Add to `~/.bashrc` or `~/.zshrc`:

```bash
# Gemini CLI with HelixLLM
gemini() {
  GEMINI_API_BASE_URL=http://localhost:8000/v1 \
  GEMINI_API_KEY=your-helixllm-api-key \
  command gemini "$@"
}
```

### Usage

```bash
# Start chat session
gemini chat

# Single query
gemini "Explain quantum computing"

# With file input
gemini -f document.txt "Summarize this document"

# Code generation
gemini --mode code "Write a Python function to sort a list"
```

---

## <a name="claude-code"></a>4. Claude Code Configuration

Claude Code supports custom endpoints through environment configuration.

### Installation

```bash
# Install Claude Code
npm install -g @anthropic-ai/claude-code

# Or via Homebrew (macOS)
brew install claude-code
```

### Configuration

Claude Code uses environment variables for OpenAI-compatible endpoints:

```bash
# Add to ~/.bashrc, ~/.zshrc, or ~/.bash_profile

# Required environment variables
export ANTHROPIC_BASE_URL=http://localhost:8000/v1
export ANTHROPIC_API_KEY=your-helixllm-api-key-or-any-value

# Optional: Override model
export CLAUDE_CODE_MODEL=helix-llm

# Optional: Enable features
export CLAUDE_CODE_STREAMING=true
export CLAUDE_CODE_TOOLS=true
```

### Configuration File

Create `~/.claude-code/config.json`:

```json
{
  "api": {
    "provider": "openai-compatible",
    "baseUrl": "http://localhost:8000/v1",
    "apiKey": "your-helixllm-api-key-or-any-value",
    "model": "helix-llm"
  },
  "behavior": {
    "temperature": 0.7,
    "maxTokens": 4096,
    "streaming": true,
    "toolsEnabled": true
  },
  "ui": {
    "theme": "auto",
    "showTokenCount": true,
    "confirmDestructiveActions": true
  }
}
```

### Project-Specific Configuration

Create `.claude-code.json` in project root:

```json
{
  "api": {
    "baseUrl": "http://localhost:8000/v1",
    "model": "helix-llm"
  },
  "context": {
    "include": ["src/**/*", "tests/**/*"],
    "exclude": ["node_modules/**", ".git/**", "dist/**"]
  },
  "commands": {
    "review": {
      "prompt": "Review this code for best practices and potential issues."
    },
    "test": {
      "prompt": "Generate unit tests for the selected code."
    }
  }
}
```

### Wrapper Script

Create `~/bin/claude-helix`:

```bash
#!/bin/bash
# Claude Code wrapper for HelixLLM

export ANTHROPIC_BASE_URL=http://localhost:8000/v1
export ANTHROPIC_API_KEY=${HELIXLLM_API_KEY:-"helix-local"}
export CLAUDE_CODE_MODEL=helix-llm

exec claude-code "$@"
```

Make it executable:

```bash
chmod +x ~/bin/claude-helix
```

### Usage

```bash
# Start interactive session
claude-code

# Or with wrapper
claude-helix

# Execute command
claude-code "Review the src/ directory"

# With context
claude-code -c src/main.py "Explain this file"

# Git integration
claude-code git "Write a commit message for these changes"
```

---

## <a name="environment-variables"></a>5. Environment Variables Reference

### HelixLLM Server Variables

```bash
# Server configuration
export HELIXLLM_HOST=0.0.0.0
export HELIXLLM_PORT=8000
export HELIXLLM_API_KEY=your-secret-api-key  # Leave empty for no auth

# Model configuration
export HELIXLLM_MODEL=helix-llm
export HELIXLLM_VERSION=1.0.0
export HELIXLLM_MAX_TOKENS=4096
export HELIXLLM_TEMPERATURE=0.7

# Feature flags
export HELIXLLM_ENABLE_STREAMING=true
export HELIXLLM_LOG_LEVEL=INFO

# CORS configuration
export HELIXLLM_CORS_ORIGINS="*"

# Rate limiting (optional)
export HELIXLLM_RATE_LIMIT=false
export HELIXLLM_RATE_LIMIT_REQUESTS=100
export HELIXLLM_RATE_LIMIT_WINDOW=60
```

### Universal Client Configuration

Create `~/.helixllm/config.env`:

```bash
# Universal HelixLLM configuration
HELIXLLM_BASE_URL=http://localhost:8000/v1
HELIXLLM_API_KEY=your-helixllm-api-key
HELIXLLM_MODEL=helix-llm

# Source this in your shell profile:
# source ~/.helixllm/config.env
```

---

## <a name="testing"></a>6. Testing Your Setup

### Test the HelixLLM Server

```bash
# Start the server
cd /path/to/helixllm_api
pip install -r requirements.txt
python main.py

# Test in another terminal

# 1. Health check
curl http://localhost:8000/health

# 2. List models
curl http://localhost:8000/v1/models

# 3. Simple chat completion
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "helix-llm",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# 4. Streaming chat completion
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "helix-llm",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'

# 5. Test with tools
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "helix-llm",
    "messages": [{"role": "user", "content": "What is the weather?"}],
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

# 6. Test embeddings
curl -X POST http://localhost:8000/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "model": "helix-llm",
    "input": "Hello world"
  }'
```

### Test CLI Agent Integration

```bash
# Test OpenCode
opencode "Say hello"

# Test Crush
crush ask "What can you do?"

# Test Gemini CLI
gemini "Introduce yourself"

# Test Claude Code
claude-code "Hello, are you working?"
```

---

## Troubleshooting

### Common Issues

1. **Connection Refused**
   - Ensure HelixLLM server is running: `curl http://localhost:8000/health`
   - Check firewall settings
   - Verify port configuration

2. **Authentication Errors**
   - If using API key, ensure it matches `HELIXLLM_API_KEY`
   - For no-auth mode, leave `HELIXLLM_API_KEY` empty

3. **Model Not Found**
   - Use the exact model name configured in HelixLLM
   - Default is `helix-llm`

4. **Streaming Not Working**
   - Verify `HELIXLLM_ENABLE_STREAMING=true`
   - Check client supports SSE

5. **Tool Calling Issues**
   - Ensure tools are properly formatted
   - Check tool schema is valid JSON

### Debug Mode

Enable debug logging:

```bash
export HELIXLLM_LOG_LEVEL=DEBUG
python main.py
```

### Getting Help

- Check server logs for errors
- Verify OpenAI API compatibility with: `curl http://localhost:8000/v1/models`
- Test with simple requests before complex ones
