# System Architecture Documentation

## Light Local LLM System - Service Dependencies

---

## Service Dependency Graph

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              SERVICE DEPENDENCIES                           │
└─────────────────────────────────────────────────────────────────────────────┘

                                    ┌─────────────┐
                                    │   Traefik   │
                                    │  (Ingress)  │
                                    └──────┬──────┘
                                           │
                    ┌──────────────────────┼──────────────────────┐
                    │                      │                      │
                    ▼                      ▼                      ▼
           ┌─────────────┐       ┌─────────────┐       ┌─────────────┐
           │ API Gateway │       │  Grafana    │       │ Prometheus  │
           │   :8080     │       │   :3001     │       │   :9090     │
           └──────┬──────┘       └─────────────┘       └──────┬──────┘
                  │                                           │
        ┌─────────┼─────────┐                                 │
        │         │         │                                 │
        ▼         ▼         ▼                                 ▼
┌──────────┐ ┌────────┐ ┌────────┐                  ┌─────────────┐
│  RAG     │ │  MCP   │ │ Ollama │                  │ Alertmanager│
│ Service  │ │ Server │ │ :11434 │                  │   :9093     │
└────┬─────┘ └────────┘ └────────┘                  └─────────────┘
     │
     │ (depends on)
     ▼
┌──────────┐     ┌──────────┐
│ ChromaDB │◄────│  Loki    │
│  :8000   │     │  :3100   │
└──────────┘     └────┬─────┘
                      │
                      ▼
               ┌──────────┐
               │ Promtail │
               └──────────┘
```

---

## Startup Order

Services must start in the following order to ensure dependencies are available:

### Phase 1: Infrastructure (10s delay)
1. **Traefik** - Reverse proxy (no dependencies)
2. **Redis** - Caching layer (no dependencies)
3. **ChromaDB** - Vector database (no dependencies)

### Phase 2: Core Services (5s delay)
4. **Ollama** - LLM inference (no dependencies)
5. **Loki** - Log aggregation (no dependencies)
6. **Promtail** - Log shipping (depends on Loki)

### Phase 3: Application Services (10s delay)
7. **RAG Service** - Document processing (depends on ChromaDB, Ollama)
8. **MCP Server** - Tool integration (no dependencies)

### Phase 4: API Layer (5s delay)
9. **API Gateway** - Main entry point (depends on RAG, MCP, Ollama)

### Phase 5: Monitoring (immediate)
10. **Prometheus** - Metrics collection (no dependencies)
11. **Grafana** - Visualization (depends on Prometheus, Loki)
12. **Alertmanager** - Alert routing (depends on Prometheus)
13. **Node Exporter** - System metrics (no dependencies)
14. **cAdvisor** - Container metrics (no dependencies)

---

## Network Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              NETWORK TOPOLOGY                               │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                         External Network (Internet)                         │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Traefik (Port 80/443)                             │
│                    ┌─────────────────────────────────────┐                  │
│                    │  Rate Limiting │ SSL Termination    │                  │
│                    │  Routing │ Load Balancing          │                  │
│                    └─────────────────────────────────────┘                  │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                    ┌─────────────────┼─────────────────┐
                    │                 │                 │
                    ▼                 ▼                 ▼
┌─────────────────────────┐ ┌───────────────┐ ┌─────────────────┐
│     llm-network         │ │  monitoring-  │ │   Internal      │
│   (172.20.0.0/16)       │ │   network     │ │   Services      │
├─────────────────────────┤ │  (Internal)   │ │  (Isolated)     │
│ • API Gateway           │ ├───────────────┤ ├─────────────────┤
│ • RAG Service           │ │ • Prometheus  │ │ • ChromaDB      │
│ • MCP Server            │ │ • Grafana     │ │ • Ollama        │
│ • Ollama                │ │ • Loki        │ │ • Redis         │
│ • ChromaDB              │ │ • Alertmanager│ │                 │
│ • Traefik               │ │ • Node Exporter│ │                │
└─────────────────────────┘ └───────────────┘ └─────────────────┘
```

---

## Data Flow

### 1. Chat Query with RAG

```
┌─────────┐     ┌──────────┐     ┌─────────────┐     ┌──────────┐     ┌────────┐
│  User   │────▶│  Traefik │────▶│ API Gateway │────▶│  RAG     │────▶│ChromaDB│
│ Request │     │ (Proxy)  │     │  (Routing)  │     │ Service  │     │(Search)│
└─────────┘     └──────────┘     └─────────────┘     └────┬─────┘     └────┬───┘
                                                          │                │
                                                          │                │
                                                          ▼                │
                                                   ┌─────────────┐         │
                                                   │  Embed      │         │
                                                   │  Query      │         │
                                                   └──────┬──────┘         │
                                                          │                │
                                                          ▼                │
                                                   ┌─────────────┐◀────────┘
                                                   │  Retrieve   │
                                                   │  Documents  │
                                                   └──────┬──────┘
                                                          │
                                                          ▼
┌─────────┐     ┌──────────┐     ┌─────────────┐     ┌──────────┐
│  User   │◀────│  Traefik │◀────│ API Gateway │◀────│  RAG     │◀────┐
│ Response│     │ (Proxy)  │     │  (Response) │     │ Service  │     │
└─────────┘     └──────────┘     └─────────────┘     └──────────┘     │
                                                                       │
                                                                       │
┌──────────────────────────────────────────────────────────────────────┘
│
▼
┌────────┐     ┌─────────────┐
│ Ollama │◀────│  Generate   │
│ (LLM)  │     │  Response   │
└────────┘     └─────────────┘
```

### 2. Metrics Collection Flow

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Services   │────▶│  Prometheus │────▶│   Grafana   │
│ (Exporters) │     │ (Scrape)    │     │ (Dashboard) │
└─────────────┘     └──────┬──────┘     └─────────────┘
                           │
                           ▼
                    ┌─────────────┐
                    │ Alert Rules │
                    └──────┬──────┘
                           │
                           ▼
                    ┌─────────────┐
                    │ Alertmanager│
                    │  (Notify)   │
                    └─────────────┘
```

### 3. Log Aggregation Flow

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Containers │────▶│  Promtail   │────▶│    Loki     │
│   (Logs)    │     │  (Collect)  │     │  (Store)    │
└─────────────┘     └─────────────┘     └──────┬──────┘
                                               │
                                               ▼
                                        ┌─────────────┐
                                        │   Grafana   │
                                        │  (Explore)  │
                                        └─────────────┘
```

---

## Health Check Dependencies

```
Service Health Check Chain:

Traefik ──▶ API Gateway ──▶ RAG Service ──▶ ChromaDB
                │               │
                │               └──▶ Ollama
                │
                └──▶ MCP Server

If any service fails its health check:
1. Docker attempts automatic restart (max 3 attempts)
2. Alert is triggered in Prometheus
3. Notification sent via Alertmanager
4. Dependent services may degrade gracefully
```

---

## Resource Allocation

### Recommended Minimum Resources

| Service | CPU | Memory | Disk |
|---------|-----|--------|------|
| Ollama | 4 cores | 8 GB | 50 GB |
| ChromaDB | 2 cores | 4 GB | 20 GB |
| RAG Service | 2 cores | 4 GB | 5 GB |
| API Gateway | 1 core | 2 GB | 1 GB |
| Grafana | 1 core | 1 GB | 10 GB |
| Prometheus | 1 core | 2 GB | 50 GB |
| **Total** | **11 cores** | **22 GB** | **136 GB** |

### Resource Limits (Docker)

```yaml
deploy:
  resources:
    limits:
      cpus: '2.0'
      memory: 4G
    reservations:
      cpus: '0.5'
      memory: 1G
```

---

## Scaling Considerations

### Horizontal Scaling

```
┌─────────────────────────────────────────────────────────────────┐
│                        Load Balancer                            │
│                         (Traefik)                               │
└─────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
        ▼                     ▼                     ▼
┌───────────────┐   ┌───────────────┐   ┌───────────────┐
│  API Gateway  │   │  API Gateway  │   │  API Gateway  │
│   Instance 1  │   │   Instance 2  │   │   Instance 3  │
└───────┬───────┘   └───────┬───────┘   └───────┬───────┘
        │                   │                   │
        └───────────────────┼───────────────────┘
                            │
        ┌───────────────────┼───────────────────┐
        │                   │                   │
        ▼                   ▼                   ▼
┌───────────────┐   ┌───────────────┐   ┌───────────────┐
│  RAG Service  │   │  RAG Service  │   │  RAG Service  │
│   Instance 1  │   │   Instance 2  │   │   Instance 3  │
└───────────────┘   └───────────────┘   └───────────────┘

Shared:
- ChromaDB (cluster mode)
- Redis (cluster mode)
- Ollama (load balanced)
```

### Vertical Scaling

For single-machine deployment:
- Increase CPU cores for Ollama
- Add RAM for larger models
- Use SSD for ChromaDB
- Enable GPU acceleration

---

## Security Boundaries

```
┌─────────────────────────────────────────────────────────────────┐
│                         PUBLIC ZONE                             │
│  • Traefik (80/443)                                            │
│  • API Gateway (8080)                                          │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      APPLICATION ZONE                           │
│  • RAG Service                                                 │
│  • MCP Server                                                  │
│  • Ollama (internal access only)                               │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        DATA ZONE                                │
│  • ChromaDB                                                    │
│  • Redis                                                       │
│  • Volumes                                                     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     MONITORING ZONE                             │
│  • Prometheus                                                  │
│  • Grafana                                                     │
│  • Loki                                                        │
│  (Internal network only)                                       │
└─────────────────────────────────────────────────────────────────┘
```

---

## Backup Strategy

```
Backup Components:

┌─────────────────────────────────────────────────────────────────┐
│  CRITICAL (Daily)                                               │
│  ├── ChromaDB (vector data)                                    │
│  ├── Ollama models                                             │
│  └── Configuration files                                       │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  IMPORTANT (Weekly)                                             │
│  ├── Grafana dashboards                                        │
│  ├── Prometheus data                                           │
│  └── Application logs                                          │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  ARCHIVE (Monthly)                                              │
│  └── Historical metrics                                        │
└─────────────────────────────────────────────────────────────────┘
```

---

## Failure Scenarios

### Scenario 1: Ollama Failure

```
Impact:
- LLM queries fail
- RAG service degrades (no generation)

Recovery:
1. Docker auto-restarts Ollama
2. Model reloads automatically
3. Service returns to healthy

Mitigation:
- Deploy multiple Ollama instances
- Use model caching
```

### Scenario 2: ChromaDB Failure

```
Impact:
- RAG queries fail (no retrieval)
- Document indexing stops

Recovery:
1. Restart ChromaDB container
2. Data persists in volume
3. Service recovers automatically

Mitigation:
- Regular backups
- ChromaDB clustering (future)
```

### Scenario 3: API Gateway Failure

```
Impact:
- All API requests fail
- System appears down

Recovery:
1. Traefik detects unhealthy gateway
2. Auto-restart gateway
3. Traffic resumes

Mitigation:
- Multiple gateway instances
- Health check monitoring
```

---

## Performance Characteristics

### Expected Latencies

| Operation | Target | Acceptable |
|-----------|--------|------------|
| Health Check | < 100ms | < 500ms |
| LLM Inference | < 5s | < 15s |
| RAG Query | < 10s | < 30s |
| Vector Search | < 2s | < 5s |
| API Response | < 100ms | < 500ms |

### Throughput Targets

| Metric | Target | Stress Limit |
|--------|--------|--------------|
| Concurrent Users | 50 | 100 |
| Requests/Second | 10 | 50 |
| LLM Tokens/Second | 20 | 50 |
| RAG Queries/Minute | 100 | 300 |

---

## Monitoring Checklist

### Service Health
- [ ] All containers running
- [ ] Health checks passing
- [ ] No restart loops
- [ ] Resource usage normal

### Performance
- [ ] Latency within targets
- [ ] Error rate < 1%
- [ ] Queue depth manageable
- [ ] Token throughput stable

### Data Integrity
- [ ] ChromaDB responding
- [ ] No data corruption
- [ ] Backups successful
- [ ] Log aggregation working

### Security
- [ ] No unauthorized access
- [ ] Certificates valid
- [ ] Firewall rules active
- [ ] Secrets secured
