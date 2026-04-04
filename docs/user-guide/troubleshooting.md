# Troubleshooting

Common issues and their solutions when running HelixLLM.

## Server Won't Start

### "server: TLSCert and TLSKey are required for ListenAndServe"

TLS certificates are missing. Generate them:

```bash
make certs
```

This creates self-signed certificates in `./certs/`. Alternatively, point to your own certificates:

```bash
HELIX_TLS_CERT=/path/to/cert.pem
HELIX_TLS_KEY=/path/to/key.pem
```

### "error loading config"

The `.env` file has invalid syntax or a required variable is malformed. Check:

```bash
# Verify .env exists
ls -la .env

# Check for syntax errors (no spaces around = signs)
cat .env | grep -n " ="
```

### "invalid mode"

The `HELIX_MODE` value is not recognized. Valid modes: `full`, `gateway`, `brain`, `knowledge`, `agents`, `control`.

```bash
# Check your setting
grep HELIX_MODE .env

# Or use the CLI flag
./bin/helixllm --mode=full
```

### "invalid port"

Port must be between 1 and 65535:

```bash
HELIX_PORT=8443
```

### Port already in use

Another process is using the configured port:

```bash
# Find what's using the port
lsof -i :8443

# Use a different port
HELIX_PORT=9443 make dev
```

## TLS / Certificate Issues

### "curl: (60) SSL certificate problem: self-signed certificate"

Expected with self-signed certs. Use `-k` to skip verification:

```bash
curl -k https://localhost:8443/internal/health
```

For OpenAI/Anthropic SDK clients, configure them to skip TLS verification or add your self-signed CA to the system trust store.

### "tls: failed to find any PEM data"

The certificate file exists but is empty or not PEM-encoded. Regenerate:

```bash
rm -rf certs/
make certs
```

## API Errors

### HTTP 401 Unauthorized

API key authentication is enabled but the key is missing or invalid:

```bash
# Check if API keys are configured
grep HELIX_AUTH_API_KEYS .env

# Include the key in your request
curl -k -H "Authorization: Bearer your-api-key" \
  https://localhost:8443/v1/models
```

To disable authentication, leave `HELIX_AUTH_API_KEYS` empty in `.env`.

### HTTP 429 Too Many Requests

Rate limiting is active. Wait and retry, or increase the rate limit configuration.

### HTTP 400 "messages must not be empty"

The chat completion request is missing the `messages` array:

```bash
curl -k https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "...", "messages": [{"role": "user", "content": "Hello"}]}'
```

### HTTP 400 "must not be empty" (Knowledge API)

Knowledge endpoints require non-empty `content`, `collection`, and `query` fields:

```bash
# Ingest requires content and collection
curl -k -X POST https://localhost:8443/internal/knowledge/ingest \
  -d '{"content": "Some text", "collection": "my-collection"}'

# Query requires query text
curl -k -X POST https://localhost:8443/internal/knowledge/query \
  -d '{"query": "search terms"}'
```

## LLM Provider Issues

### Local model not responding

The llama.cpp server may not be running:

```bash
# Check if it's running
curl http://localhost:50052/health

# Check the configured port
grep HELIX_LLM_LOCAL_RPC_PORT .env
```

### OpenAI/Anthropic returning errors

Verify your API keys are set and valid:

```bash
grep HELIX_LLM_OPENAI_KEY .env
grep HELIX_LLM_ANTHROPIC_KEY .env
```

Check the log output for provider-specific error messages. Set `HELIX_LOG_LEVEL=debug` for detailed request/response logging.

### Model not found

The requested model may not be available from any configured provider:

```bash
# List available models
curl -k https://localhost:8443/v1/models

# Check default provider
grep HELIX_LLM_DEFAULT_PROVIDER .env
```

## Multi-Host Issues

### SSH connection refused

```bash
# Test SSH manually
ssh -i ~/.ssh/id_ed25519 milosvasic@thinker.local

# Check key permissions
chmod 600 ~/.ssh/id_ed25519
chmod 700 ~/.ssh

# Verify authorized_keys on remote host
ssh user@host "cat ~/.ssh/authorized_keys"
```

### Host unreachable during probe

```bash
# Check hostname resolution
ping thinker.local

# Or add to /etc/hosts
echo "192.168.1.100 thinker.local" | sudo tee -a /etc/hosts
```

### Container runtime not found on remote host

```bash
# Check remotely
ssh user@host "which podman || which docker"

# Install Podman
ssh user@host "sudo apt install podman"  # Debian/Ubuntu
ssh user@host "sudo dnf install podman"  # Fedora/RHEL
```

## Build Issues

### Submodule errors

```bash
# Re-initialize all submodules
make deps

# Or manually
git submodule update --init --recursive
go mod tidy
```

### "module not found" during build

Submodules use `replace` directives in `go.mod` to point to local paths. Ensure submodules are checked out:

```bash
ls submodules/
git submodule status
```

### Test failures

```bash
# Run tests with verbose output
make test-unit

# Check coverage
make coverage

# Run specific package tests
go test -v ./internal/gateway/...
```

## Logging and Debugging

### Enable debug logging

```bash
HELIX_LOG_LEVEL=debug make dev
```

### Use JSON log format for parsing

```bash
HELIX_LOG_FORMAT=json make dev
```

### Trace a specific request

Include a custom request ID:

```bash
curl -k -H "X-Request-Id: my-trace-123" \
  https://localhost:8443/v1/models
```

Search logs for `my-trace-123` to follow the request through the system.

### Enable tracing output

```bash
HELIX_OTEL_EXPORTER=stdout make dev
```

This prints OpenTelemetry traces to stdout for debugging.

## Performance Issues

### High latency on first request

The first request may be slower due to:
- TLS handshake (subsequent requests reuse connections)
- Model loading on the llama.cpp server
- Cold start of embedding models

### Memory usage growing

Check for:
- Large conversation contexts (sessions are stored in memory)
- Accumulated vector data in the in-memory store
- Set appropriate limits and monitor with Prometheus metrics

## Getting Help

1. Check the logs with `HELIX_LOG_LEVEL=debug`
2. Verify configuration with `.env.example` as reference
3. Check the health endpoint: `curl -k https://localhost:8443/internal/health`
4. Review the [Configuration Reference](configuration.md) for valid values
