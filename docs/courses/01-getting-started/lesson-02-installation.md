# Lesson 2: Installation

**Duration:** 20 minutes
**Prerequisites:** None
**Learning Objectives:**
- Install all prerequisites for building HelixLLM
- Clone the repository with all 37 submodules
- Build the binary and generate TLS certificates
- Start the server and verify it is running

---

## Scene 1: Prerequisites (4 min)

**Narration:** "Before we build HelixLLM, we need a few tools installed. Let me walk through each one."

**Screen:** Show the prerequisites list in a terminal.

**Key points:**
- **Go 1.24+** -- the project uses Go 1.26.1 module syntax but builds with 1.24 and above
- **Git** -- with submodule support for the 37 dependency modules
- **OpenSSL** -- used to generate self-signed TLS certificates for local development
- **Podman or Docker** (optional) -- for container builds and multi-host deployment

**Demo steps:**

```bash
# Verify Go version
go version

# Verify Git
git --version

# Verify OpenSSL
openssl version
```

**Narration:** "Optional tools include golangci-lint for linting and goimports for formatting. You do not need these to build and run, but they are useful for development."

```bash
# Optional: verify lint and format tools
golangci-lint --version
goimports -h 2>&1 | head -1
```

---

## Scene 2: Cloning the Repository (4 min)

**Narration:** "HelixLLM uses 37 Git submodules that provide the production infrastructure -- configuration, event bus, streaming, security, and more. The easiest way to get everything is to clone with the recurse-submodules flag."

**Demo steps:**

```bash
# Clone with all submodules
git clone --recurse-submodules https://github.com/HelixDevelopment/HelixLLM.git
cd HelixLLM
```

**Narration:** "If you already cloned without the submodules flag, run make deps to initialize them."

```bash
# Alternative: initialize submodules after cloning
make deps
```

**Narration:** "This runs git submodule update --init --recursive followed by go mod tidy. The submodules live under the submodules/ directory and are imported into go.mod via replace directives."

**Screen:** Show the submodules directory listing.

```bash
ls submodules/
```

**Key points:**
- Each submodule has its own CLAUDE.md with documentation
- Submodules form the `digital.vasic.*` and `dev.helix.*` module ecosystem
- `go.mod` maps each module to a local submodule path via `replace` directives

---

## Scene 3: Building the Binary (4 min)

**Narration:** "Building HelixLLM is a single make command. The output is a statically-linked Go binary."

**Demo steps:**

```bash
# Build the binary
make build
```

**Narration:** "The binary is written to bin/helixllm. It uses ldflags to strip debug symbols, producing a smaller production-ready binary."

```bash
# Verify the binary exists
ls -lh bin/helixllm

# Check it runs
./bin/helixllm --help
```

**Key points:**
- Output: `bin/helixllm`
- Build flags: `-ldflags="-s -w"` strips debug symbols
- Single binary contains all layers -- no external dependencies at runtime

---

## Scene 4: Generating TLS Certificates (3 min)

**Narration:** "HelixLLM requires TLS for all connections. For local development, we generate a self-signed certificate using an elliptic curve key."

**Demo steps:**

```bash
# Generate self-signed TLS certificates
make certs
```

**Narration:** "This creates two files in the certs directory: cert.pem and key.pem. The certificate uses the P-256 elliptic curve, is valid for 365 days, and includes Subject Alternative Names for localhost."

```bash
# Verify certificates were created
ls -la certs/

# Inspect the certificate
openssl x509 -in certs/cert.pem -text -noout | head -20
```

**Key points:**
- Elliptic curve P-256 for fast, secure TLS
- SANs include `DNS:localhost`, `DNS:nezha.local`, and `IP:127.0.0.1`
- 365-day validity, no passphrase for automated startup
- For production, use certificates from a trusted CA

---

## Scene 5: First Run (4 min)

**Narration:** "Now let us start HelixLLM for the first time. The make dev target handles everything -- it generates certificates if missing and starts the server in full mode."

**Demo steps:**

```bash
# Start in development mode
make dev
```

**Narration:** "You should see output indicating the server is listening on port 8443. The mode is set to full, meaning all layers are active."

**Screen:** Show expected output:

```
[GIN] mode=release
INFO starting HelixLLM                mode=full
INFO server listening                 addr=0.0.0.0:8443
```

**Narration:** "In another terminal, let us verify the server is running with a health check."

```bash
# Verify the server is running
curl -k https://localhost:8443/internal/health
```

**Expected response:**

```json
{
  "status": "healthy",
  "checks": []
}
```

**Key points:**
- `make dev` is the quickest way to start developing
- The `-k` flag tells curl to accept the self-signed certificate
- The health endpoint returns HTTP 200 when all subsystems are healthy

---

## Scene 6: Configuration File (2 min)

**Narration:** "Before we close, let me mention the configuration file. Copy the example environment file to create your local configuration."

**Demo steps:**

```bash
# Copy example configuration
cp .env.example .env

# View the defaults
cat .env
```

**Narration:** "The defaults work out of the box for single-host development. We will explore all the configuration options in detail in Lesson 4."

**Key points:**
- `.env` file is gitignored -- never commit secrets
- Default mode is `full`, default port is `8443`
- Cloud provider keys are optional -- leave empty for local-only operation

---

## Exercises

1. Install the prerequisites and clone HelixLLM with all submodules on your machine
2. Build the binary with `make build` and verify its size with `ls -lh bin/helixllm`
3. Start the server with `make dev` and confirm the health endpoint returns healthy
