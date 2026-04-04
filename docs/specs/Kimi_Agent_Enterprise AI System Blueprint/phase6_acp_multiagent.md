# Phase 6: ACP (Agent Communication Protocol) Multi-Agent System

## Complete Implementation Guide for Local LLM Multi-Agent Collaboration

---

## Table of Contents

1. [ACP Architecture Overview](#1-acp-architecture-overview)
2. [Agent Types Definition](#2-agent-types-definition)
3. [ACP Protocol Implementation](#3-acp-protocol-implementation)
4. [Agent Registration System](#4-agent-registration-system)
5. [Task Delegation Pattern](#5-task-delegation-pattern)
6. [Inter-Agent Communication](#6-inter-agent-communication)
7. [Specialized Agent Implementations](#7-specialized-agent-implementations)
8. [Coordination Strategies](#8-coordination-strategies)
9. [Configuration Files](#9-configuration-files)
10. [Usage Examples](#10-usage-examples)

---

## 1. ACP Architecture Overview

### 1.1 Protocol Specification

The Agent Communication Protocol (ACP) is a lightweight, message-based protocol designed for coordinating multiple AI agents in a local LLM system.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         ACP PROTOCOL STACK                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐        │
│  │   Agent     │  │   Agent     │  │   Agent     │  │   Agent     │        │
│  │   Layer     │  │   Layer     │  │   Layer     │  │   Layer     │        │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘        │
│         │                │                │                │                │
│  ┌──────┴────────────────┴────────────────┴────────────────┴──────┐        │
│  │                    Message Bus / Channel                       │        │
│  └──────┬────────────────┬────────────────┬────────────────┬──────┘        │
│         │                │                │                │                │
│  ┌──────┴──────┐  ┌──────┴──────┐  ┌──────┴──────┐  ┌──────┴──────┐       │
│  │  Registry   │  │   Task      │  │   Event     │  │   Health    │       │
│  │  Service    │  │   Manager   │  │   System    │  │   Monitor   │       │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘       │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1.2 Core Components

| Component | Description | Responsibility |
|-----------|-------------|----------------|
| **Agent** | Autonomous entity with specific capabilities | Execute tasks, communicate with peers |
| **Message Bus** | Central communication channel | Route messages between agents |
| **Registry** | Service discovery mechanism | Track available agents and capabilities |
| **Task Manager** | Task lifecycle management | Create, assign, track, and complete tasks |
| **Event System** | Pub/sub notification system | Broadcast events to subscribed agents |
| **Health Monitor** | Agent status tracking | Monitor agent availability and health |

### 1.3 Message Types and Formats

```python
# ACP Message Format (JSON)
{
    "header": {
        "message_id": "uuid-v4",
        "message_type": "REQUEST|RESPONSE|EVENT|BROADCAST",
        "sender_id": "agent-uuid",
        "recipient_id": "agent-uuid|broadcast",
        "timestamp": "ISO-8601",
        "correlation_id": "uuid-v4",
        "priority": 1-10,
        "ttl": 300
    },
    "payload": {
        "action": "task.create|task.complete|agent.register|...",
        "data": { ... },
        "metadata": { ... }
    },
    "signature": "hmac-sha256"
}
```

### 1.4 Communication Patterns

```
┌─────────────────────────────────────────────────────────────────┐
│                  COMMUNICATION PATTERNS                          │
├─────────────────────────────────────────────────────────────────┤
│  1. DIRECT MESSAGE                                               │
│     ┌─────┐              ┌─────┐                                │
│     │  A  │─────────────▶│  B  │                                │
│     └─────┘              └─────┘                                │
│  2. REQUEST-RESPONSE                                             │
│     ┌─────┐   Request    ┌─────┐                                │
│     │  A  │─────────────▶│  B  │                                │
│     │     │◀─────────────│     │                                │
│     └─────┘   Response   └─────┘                                │
│  3. BROADCAST                                                    │
│         ┌─────┐                                                  │
│         │  A  │                                                  │
│         └──┬──┘                                                  │
│            │                                                     │
│      ┌─────┼─────┐                                               │
│      ▼     ▼     ▼                                               │
│    ┌───┐ ┌───┐ ┌───┐                                             │
│    │ B │ │ C │ │ D │                                             │
│    └───┘ └───┘ └───┘                                             │
│  4. PUBLISH-SUBSCRIBE                                            │
│     ┌─────┐  Event:task.created  ┌─────┐                        │
│     │  A  │─────────────────────▶│ B,C │                         │
│     └─────┘                      └─────┘                        │
└─────────────────────────────────────────────────────────────────┘
```

---

## 2. Agent Types Definition

### 2.1 Agent Hierarchy

```
┌─────────────────────────────────────────────────────────────────┐
│                      AGENT HIERARCHY                             │
├─────────────────────────────────────────────────────────────────┤
│                         ┌─────────────┐                          │
│                         │ BaseAgent   │                          │
│                         │ (Abstract)  │                          │
│                         └──────┬──────┘                          │
│        ┌───────────────────────┼───────────────────────┐        │
│        │                       │                       │        │
│   ┌────┴────┐            ┌────┴────┐            ┌────┴────┐    │
│   │Orchestra│            │Specializ│            │Support  │    │
│   │-tor     │            │-ed      │            │Agents   │    │
│   └────┬────┘            └────┬────┘            └────┬────┘    │
│   ┌────┴────┐    ┌──────┬────┴────┬──────┐    ┌────┴────┐     │
│   │Main     │    │Code  │Research │Review│    │Health   │     │
│   │Orchestra│    │Agent │Agent    │Agent │    │Monitor  │     │
│   └─────────┘    └──────┴─────────┴──────┘    └─────────┘     │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 Agent Capabilities Matrix

| Agent Type | Primary Role | Capabilities | LLM Requirements |
|------------|--------------|--------------|------------------|
| **MainOrchestrator** | Central coordinator | Task routing, delegation, aggregation | Medium (7B params) |
| **CodeAgent** | Code operations | Analysis, generation, refactoring | Large (13B+ params) |
| **ResearchAgent** | Information gathering | Web search, documentation, summarization | Medium (7B params) |
| **ReviewAgent** | Quality assurance | Code review, testing, validation | Medium (7B params) |
| **DocAgent** | Documentation | Doc generation, API docs, tutorials | Small-Medium (3-7B) |

---

## 3. ACP Protocol Implementation

### 3.1 Core ACP Module (`acp_core.py`)

```python
"""ACP Core Module - Agent Communication Protocol Implementation"""

import asyncio
import json
import uuid
import hashlib
import hmac
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from typing import Dict, List, Optional, Callable, Any, Set
from collections import defaultdict
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("ACP")


class MessageType(Enum):
    REQUEST = "request"
    RESPONSE = "response"
    EVENT = "event"
    BROADCAST = "broadcast"
    HEARTBEAT = "heartbeat"
    ERROR = "error"


class MessagePriority(Enum):
    CRITICAL = 1
    HIGH = 3
    NORMAL = 5
    LOW = 7
    BACKGROUND = 10


@dataclass
class ACPMessage:
    message_id: str = field(default_factory=lambda: str(uuid.uuid4()))
    message_type: MessageType = MessageType.REQUEST
    sender_id: str = ""
    recipient_id: str = ""
    timestamp: str = field(default_factory=lambda: datetime.utcnow().isoformat())
    correlation_id: Optional[str] = None
    priority: MessagePriority = MessagePriority.NORMAL
    ttl: int = 300
    action: str = ""
    data: Dict[str, Any] = field(default_factory=dict)
    metadata: Dict[str, Any] = field(default_factory=dict)
    signature: Optional[str] = None
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "header": {
                "message_id": self.message_id,
                "message_type": self.message_type.value,
                "sender_id": self.sender_id,
                "recipient_id": self.recipient_id,
                "timestamp": self.timestamp,
                "correlation_id": self.correlation_id,
                "priority": self.priority.value,
                "ttl": self.ttl
            },
            "payload": {
                "action": self.action,
                "data": self.data,
                "metadata": self.metadata
            },
            "signature": self.signature
        }
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'ACPMessage':
        header = data.get("header", {})
        payload = data.get("payload", {})
        return cls(
            message_id=header.get("message_id", str(uuid.uuid4())),
            message_type=MessageType(header.get("message_type", "request")),
            sender_id=header.get("sender_id", ""),
            recipient_id=header.get("recipient_id", ""),
            timestamp=header.get("timestamp", datetime.utcnow().isoformat()),
            correlation_id=header.get("correlation_id"),
            priority=MessagePriority(header.get("priority", 5)),
            ttl=header.get("ttl", 300),
            action=payload.get("action", ""),
            data=payload.get("data", {}),
            metadata=payload.get("metadata", {}),
            signature=data.get("signature")
        )
    
    def to_json(self) -> str:
        return json.dumps(self.to_dict())
    
    @classmethod
    def from_json(cls, json_str: str) -> 'ACPMessage':
        return cls.from_dict(json.loads(json_str))
    
    def sign(self, secret_key: str) -> None:
        message_str = f"{self.sender_id}:{self.recipient_id}:{self.action}:{self.timestamp}"
        self.signature = hmac.new(
            secret_key.encode(), message_str.encode(), hashlib.sha256
        ).hexdigest()
    
    def verify(self, secret_key: str) -> bool:
        if not self.signature:
            return True
        message_str = f"{self.sender_id}:{self.recipient_id}:{self.action}:{self.timestamp}"
        expected = hmac.new(
            secret_key.encode(), message_str.encode(), hashlib.sha256
        ).hexdigest()
        return hmac.compare_digest(self.signature, expected)


class ACPChannel:
    def __init__(self):
        self._subscribers: Dict[str, Set[Callable]] = defaultdict(set)
        self._message_queue: asyncio.Queue = asyncio.Queue()
        self._running = False
        self._task: Optional[asyncio.Task] = None
    
    async def start(self):
        self._running = True
        self._task = asyncio.create_task(self._dispatch_loop())
        logger.info("ACP Channel started")
    
    async def stop(self):
        self._running = False
        if self._task:
            self._task.cancel()
            try:
                await self._task
            except asyncio.CancelledError:
                pass
        logger.info("ACP Channel stopped")
    
    async def _dispatch_loop(self):
        while self._running:
            try:
                message = await asyncio.wait_for(self._message_queue.get(), timeout=1.0)
                await self._dispatch(message)
            except asyncio.TimeoutError:
                continue
            except Exception as e:
                logger.error(f"Dispatch error: {e}")
    
    async def _dispatch(self, message: ACPMessage):
        recipient = message.recipient_id
        if recipient == "broadcast":
            for agent_id, callbacks in self._subscribers.items():
                if agent_id != message.sender_id:
                    for callback in callbacks:
                        try:
                            asyncio.create_task(callback(message))
                        except Exception as e:
                            logger.error(f"Callback error: {e}")
        else:
            for callback in self._subscribers.get(recipient, set()):
                try:
                    asyncio.create_task(callback(message))
                except Exception as e:
                    logger.error(f"Callback error: {e}")
    
    def subscribe(self, agent_id: str, callback: Callable):
        self._subscribers[agent_id].add(callback)
        logger.debug(f"Agent {agent_id} subscribed")
    
    def unsubscribe(self, agent_id: str, callback: Callable):
        if agent_id in self._subscribers:
            self._subscribers[agent_id].discard(callback)
    
    async def send(self, message: ACPMessage):
        await self._message_queue.put(message)
    
    def get_stats(self) -> Dict[str, Any]:
        return {
            "subscribers": len(self._subscribers),
            "queue_size": self._message_queue.qsize(),
            "running": self._running
        }


class AgentCapability:
    def __init__(self, name: str, description: str, 
                 parameters: Optional[Dict] = None,
                 returns: Optional[Dict] = None):
        self.name = name
        self.description = description
        self.parameters = parameters or {}
        self.returns = returns or {}
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "name": self.name,
            "description": self.description,
            "parameters": self.parameters,
            "returns": self.returns
        }


class AgentStatus(Enum):
    INITIALIZING = "initializing"
    IDLE = "idle"
    BUSY = "busy"
    ERROR = "error"
    SHUTDOWN = "shutdown"


class BaseAgent(ABC):
    def __init__(self, agent_id: str, name: str, 
                 capabilities: List[AgentCapability] = None):
        self.agent_id = agent_id
        self.name = name
        self.capabilities = capabilities or []
        self.status = AgentStatus.INITIALIZING
        self.channel: Optional[ACPChannel] = None
        self._message_handlers: Dict[str, Callable] = {}
        self._pending_requests: Dict[str, asyncio.Future] = {}
        self._secret_key: Optional[str] = None
        self._register_default_handlers()
    
    def _register_default_handlers(self):
        self.register_handler("ping", self._handle_ping)
        self.register_handler("get_capabilities", self._handle_get_capabilities)
        self.register_handler("get_status", self._handle_get_status)
    
    def register_handler(self, action: str, handler: Callable):
        self._message_handlers[action] = handler
    
    async def _handle_ping(self, message: ACPMessage) -> ACPMessage:
        return ACPMessage(
            message_type=MessageType.RESPONSE,
            sender_id=self.agent_id,
            recipient_id=message.sender_id,
            correlation_id=message.message_id,
            action="pong",
            data={"timestamp": datetime.utcnow().isoformat()}
        )
    
    async def _handle_get_capabilities(self, message: ACPMessage) -> ACPMessage:
        return ACPMessage(
            message_type=MessageType.RESPONSE,
            sender_id=self.agent_id,
            recipient_id=message.sender_id,
            correlation_id=message.message_id,
            action="capabilities",
            data={"capabilities": [c.to_dict() for c in self.capabilities]}
        )
    
    async def _handle_get_status(self, message: ACPMessage) -> ACPMessage:
        return ACPMessage(
            message_type=MessageType.RESPONSE,
            sender_id=self.agent_id,
            recipient_id=message.sender_id,
            correlation_id=message.message_id,
            action="status",
            data={"status": self.status.value}
        )
    
    async def connect(self, channel: ACPChannel):
        self.channel = channel
        channel.subscribe(self.agent_id, self._on_message)
        self.status = AgentStatus.IDLE
        logger.info(f"Agent {self.name} ({self.agent_id}) connected")
    
    async def disconnect(self):
        if self.channel:
            self.channel.unsubscribe(self.agent_id, self._on_message)
        self.status = AgentStatus.SHUTDOWN
        logger.info(f"Agent {self.name} disconnected")
    
    async def _on_message(self, message: ACPMessage):
        try:
            if self._secret_key and not message.verify(self._secret_key):
                logger.warning(f"Invalid signature from {message.sender_id}")
                return
            msg_time = datetime.fromisoformat(message.timestamp)
            if (datetime.utcnow() - msg_time).seconds > message.ttl:
                logger.debug(f"Message expired: {message.message_id}")
                return
            if message.message_type == MessageType.REQUEST:
                await self._handle_request(message)
            elif message.message_type == MessageType.RESPONSE:
                await self._handle_response(message)
            elif message.message_type == MessageType.EVENT:
                await self._handle_event(message)
        except Exception as e:
            logger.error(f"Error handling message: {e}")
            await self._send_error(message.sender_id, str(e), message.message_id)
    
    async def _handle_request(self, message: ACPMessage):
        handler = self._message_handlers.get(message.action)
        if handler:
            self.status = AgentStatus.BUSY
            try:
                response = await handler(message)
                if response:
                    await self.send_message(response)
            finally:
                self.status = AgentStatus.IDLE
        else:
            await self._send_error(message.sender_id, f"Unknown action: {message.action}", message.message_id)
    
    async def _handle_response(self, message: ACPMessage):
        if message.correlation_id in self._pending_requests:
            future = self._pending_requests.pop(message.correlation_id)
            if not future.done():
                future.set_result(message)
    
    async def _handle_event(self, message: ACPMessage):
        pass
    
    async def _send_error(self, recipient_id: str, error: str, correlation_id: Optional[str] = None):
        error_msg = ACPMessage(
            message_type=MessageType.ERROR,
            sender_id=self.agent_id,
            recipient_id=recipient_id,
            correlation_id=correlation_id,
            action="error",
            data={"error": error}
        )
        await self.send_message(error_msg)
    
    async def send_message(self, message: ACPMessage) -> None:
        if self.channel:
            await self.channel.send(message)
    
    async def send_request(self, recipient_id: str, action: str, 
                          data: Dict[str, Any], timeout: float = 30.0) -> Optional[ACPMessage]:
        request = ACPMessage(
            message_type=MessageType.REQUEST,
            sender_id=self.agent_id,
            recipient_id=recipient_id,
            action=action,
            data=data
        )
        future = asyncio.Future()
        self._pending_requests[request.message_id] = future
        try:
            await self.send_message(request)
            return await asyncio.wait_for(future, timeout=timeout)
        except asyncio.TimeoutError:
            self._pending_requests.pop(request.message_id, None)
            return None
    
    async def broadcast(self, action: str, data: Dict[str, Any]):
        message = ACPMessage(
            message_type=MessageType.BROADCAST,
            sender_id=self.agent_id,
            recipient_id="broadcast",
            action=action,
            data=data
        )
        await self.send_message(message)
    
    @abstractmethod
    async def initialize(self):
        pass
    
    @abstractmethod
    async def shutdown(self):
        pass
    
    def get_info(self) -> Dict[str, Any]:
        return {
            "agent_id": self.agent_id,
            "name": self.name,
            "status": self.status.value,
            "capabilities": [c.to_dict() for c in self.capabilities]
        }
```


---

## 4. Agent Registration System

### 4.1 Registry Service (`acp_registry.py`)

```python
"""ACP Registry Service - Agent Registration and Discovery"""

import asyncio
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from typing import Dict, List, Optional, Set, Any
from collections import defaultdict
import logging

logger = logging.getLogger("ACP.Registry")


@dataclass
class AgentRegistration:
    agent_id: str
    name: str
    capabilities: List[Dict[str, Any]]
    endpoint: Optional[str] = None
    metadata: Dict[str, Any] = field(default_factory=dict)
    registered_at: datetime = field(default_factory=datetime.utcnow)
    last_heartbeat: datetime = field(default_factory=datetime.utcnow)
    status: str = "active"
    health_score: float = 1.0


class ACPRegistry:
    def __init__(self, heartbeat_timeout: int = 60):
        self._agents: Dict[str, AgentRegistration] = {}
        self._capabilities_index: Dict[str, Set[str]] = defaultdict(set)
        self._heartbeat_timeout = heartbeat_timeout
        self._health_check_task: Optional[asyncio.Task] = None
        self._running = False
    
    async def start(self):
        self._running = True
        self._health_check_task = asyncio.create_task(self._health_check_loop())
        logger.info("Registry service started")
    
    async def stop(self):
        self._running = False
        if self._health_check_task:
            self._health_check_task.cancel()
            try:
                await self._health_check_task
            except asyncio.CancelledError:
                pass
        logger.info("Registry service stopped")
    
    async def _health_check_loop(self):
        while self._running:
            try:
                await self._check_agent_health()
                await asyncio.sleep(self._heartbeat_timeout // 2)
            except Exception as e:
                logger.error(f"Health check error: {e}")
                await asyncio.sleep(5)
    
    async def _check_agent_health(self):
        now = datetime.utcnow()
        timeout = timedelta(seconds=self._heartbeat_timeout)
        for agent_id, reg in list(self._agents.items()):
            time_since_heartbeat = now - reg.last_heartbeat
            if time_since_heartbeat > timeout * 2:
                reg.status = "dead"
                reg.health_score = 0.0
                logger.warning(f"Agent {agent_id} marked as dead")
            elif time_since_heartbeat > timeout:
                reg.status = "unhealthy"
                reg.health_score = max(0.0, reg.health_score - 0.3)
                logger.warning(f"Agent {agent_id} is unhealthy")
    
    def register(self, agent_id: str, name: str, 
                 capabilities: List[Dict[str, Any]],
                 endpoint: Optional[str] = None,
                 metadata: Optional[Dict[str, Any]] = None) -> bool:
        if agent_id in self._agents:
            logger.warning(f"Agent {agent_id} already registered, updating")
        registration = AgentRegistration(
            agent_id=agent_id, name=name, capabilities=capabilities,
            endpoint=endpoint, metadata=metadata or {}
        )
        self._agents[agent_id] = registration
        for cap in capabilities:
            cap_name = cap.get("name", "unknown")
            self._capabilities_index[cap_name].add(agent_id)
        logger.info(f"Agent {name} ({agent_id}) registered with {len(capabilities)} capabilities")
        return True
    
    def deregister(self, agent_id: str) -> bool:
        if agent_id not in self._agents:
            logger.warning(f"Agent {agent_id} not found for deregistration")
            return False
        reg = self._agents.pop(agent_id)
        for cap in reg.capabilities:
            cap_name = cap.get("name", "unknown")
            self._capabilities_index[cap_name].discard(agent_id)
        logger.info(f"Agent {reg.name} ({agent_id}) deregistered")
        return True
    
    def update_heartbeat(self, agent_id: str) -> bool:
        if agent_id not in self._agents:
            return False
        reg = self._agents[agent_id]
        reg.last_heartbeat = datetime.utcnow()
        reg.health_score = min(1.0, reg.health_score + 0.1)
        if reg.status in ("unhealthy", "dead"):
            reg.status = "active"
            logger.info(f"Agent {agent_id} recovered")
        return True
    
    def get_agent(self, agent_id: str) -> Optional[AgentRegistration]:
        return self._agents.get(agent_id)
    
    def get_all_agents(self, status_filter: Optional[str] = None) -> List[AgentRegistration]:
        agents = list(self._agents.values())
        if status_filter:
            agents = [a for a in agents if a.status == status_filter]
        return agents
    
    def find_by_capability(self, capability: str, min_health: float = 0.5) -> List[AgentRegistration]:
        agent_ids = self._capabilities_index.get(capability, set())
        agents = [self._agents[aid] for aid in agent_ids if aid in self._agents]
        agents = [a for a in agents if a.health_score >= min_health]
        agents.sort(key=lambda a: a.health_score, reverse=True)
        return agents
    
    def find_by_capabilities(self, capabilities: List[str], require_all: bool = True,
                            min_health: float = 0.5) -> List[AgentRegistration]:
        if not capabilities:
            return []
        if require_all:
            agent_sets = [self._capabilities_index.get(cap, set()) for cap in capabilities]
            if not agent_sets:
                return []
            agent_ids = set.intersection(*agent_sets)
        else:
            agent_ids = set()
            for cap in capabilities:
                agent_ids.update(self._capabilities_index.get(cap, set()))
        agents = [self._agents[aid] for aid in agent_ids if aid in self._agents]
        agents = [a for a in agents if a.health_score >= min_health]
        agents.sort(key=lambda a: a.health_score, reverse=True)
        return agents
    
    def get_statistics(self) -> Dict[str, Any]:
        status_counts = defaultdict(int)
        for reg in self._agents.values():
            status_counts[reg.status] += 1
        return {
            "total_agents": len(self._agents),
            "status_breakdown": dict(status_counts),
            "total_capabilities": len(self._capabilities_index),
            "capabilities": list(self._capabilities_index.keys())
        }
```

### 4.2 Registration Client (`acp_registration_client.py`)

```python
"""ACP Registration Client - Agent-side registration utilities"""

import asyncio
from typing import Dict, Optional, Any
import logging

logger = logging.getLogger("ACP.RegistrationClient")


class RegistrationClient:
    def __init__(self, agent, registry, heartbeat_interval: int = 30):
        self.agent = agent
        self.registry = registry
        self.heartbeat_interval = heartbeat_interval
        self._heartbeat_task: Optional[asyncio.Task] = None
        self._registered = False
    
    async def register(self, endpoint: Optional[str] = None,
                      metadata: Optional[Dict[str, Any]] = None) -> bool:
        capabilities = [c.to_dict() for c in self.agent.capabilities]
        success = self.registry.register(
            agent_id=self.agent.agent_id,
            name=self.agent.name,
            capabilities=capabilities,
            endpoint=endpoint,
            metadata=metadata
        )
        if success:
            self._registered = True
            self._heartbeat_task = asyncio.create_task(self._heartbeat_loop())
            logger.info(f"Agent {self.agent.name} registered successfully")
        return success
    
    async def deregister(self) -> bool:
        if self._heartbeat_task:
            self._heartbeat_task.cancel()
            try:
                await self._heartbeat_task
            except asyncio.CancelledError:
                pass
        success = self.registry.deregister(self.agent.agent_id)
        self._registered = False
        if success:
            logger.info(f"Agent {self.agent.name} deregistered")
        return success
    
    async def _heartbeat_loop(self):
        while self._registered:
            try:
                self.registry.update_heartbeat(self.agent.agent_id)
                await asyncio.sleep(self.heartbeat_interval)
            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.error(f"Heartbeat error: {e}")
                await asyncio.sleep(5)
    
    def is_registered(self) -> bool:
        return self._registered
```

---

## 5. Task Delegation Pattern

### 5.1 Task Manager (`acp_task_manager.py`)

```python
"""ACP Task Manager - Task creation, delegation, and tracking"""

import asyncio
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from typing import Dict, List, Optional, Any, Callable
import uuid
import logging

logger = logging.getLogger("ACP.TaskManager")


class TaskStatus(Enum):
    PENDING = "pending"
    ASSIGNED = "assigned"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELLED = "cancelled"


class TaskPriority(Enum):
    CRITICAL = 1
    HIGH = 2
    NORMAL = 3
    LOW = 4
    BACKGROUND = 5


@dataclass
class Task:
    task_id: str = field(default_factory=lambda: str(uuid.uuid4()))
    parent_id: Optional[str] = None
    title: str = ""
    description: str = ""
    task_type: str = ""
    status: TaskStatus = TaskStatus.PENDING
    priority: TaskPriority = TaskPriority.NORMAL
    creator_id: str = ""
    assignee_id: Optional[str] = None
    input_data: Dict[str, Any] = field(default_factory=dict)
    output_data: Dict[str, Any] = field(default_factory=dict)
    created_at: datetime = field(default_factory=datetime.utcnow)
    started_at: Optional[datetime] = None
    completed_at: Optional[datetime] = None
    deadline: Optional[datetime] = None
    progress: float = 0.0
    subtasks: List['Task'] = field(default_factory=list)
    result: Any = None
    error: Optional[str] = None
    on_progress: Optional[Callable] = None
    on_complete: Optional[Callable] = None
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "task_id": self.task_id,
            "parent_id": self.parent_id,
            "title": self.title,
            "description": self.description,
            "task_type": self.task_type,
            "status": self.status.value,
            "priority": self.priority.value,
            "creator_id": self.creator_id,
            "assignee_id": self.assignee_id,
            "input_data": self.input_data,
            "output_data": self.output_data,
            "created_at": self.created_at.isoformat(),
            "started_at": self.started_at.isoformat() if self.started_at else None,
            "completed_at": self.completed_at.isoformat() if self.completed_at else None,
            "deadline": self.deadline.isoformat() if self.deadline else None,
            "progress": self.progress,
            "subtask_count": len(self.subtasks),
            "result": self.result,
            "error": self.error
        }


class TaskManager:
    def __init__(self):
        self._tasks: Dict[str, Task] = {}
        self._task_queue: asyncio.PriorityQueue = asyncio.PriorityQueue()
        self._task_handlers: Dict[str, Callable] = {}
        self._progress_callbacks: Dict[str, List[Callable]] = defaultdict(list)
        self._completion_callbacks: Dict[str, List[Callable]] = defaultdict(list)
    
    def create_task(self, title: str, description: str, task_type: str,
                   creator_id: str, input_data: Optional[Dict[str, Any]] = None,
                   priority: TaskPriority = TaskPriority.NORMAL,
                   parent_id: Optional[str] = None,
                   deadline: Optional[datetime] = None) -> Task:
        task = Task(
            title=title, description=description, task_type=task_type,
            creator_id=creator_id, input_data=input_data or {},
            priority=priority, parent_id=parent_id, deadline=deadline
        )
        self._tasks[task.task_id] = task
        self._task_queue.put_nowait((priority.value, task.created_at, task.task_id))
        logger.info(f"Task created: {title} ({task.task_id})")
        return task
    
    def assign_task(self, task_id: str, assignee_id: str) -> bool:
        task = self._tasks.get(task_id)
        if not task:
            logger.error(f"Task {task_id} not found")
            return False
        if task.status != TaskStatus.PENDING:
            logger.error(f"Task {task_id} is not pending")
            return False
        task.assignee_id = assignee_id
        task.status = TaskStatus.ASSIGNED
        task.started_at = datetime.utcnow()
        logger.info(f"Task {task_id} assigned to {assignee_id}")
        return True
    
    def start_task(self, task_id: str) -> bool:
        task = self._tasks.get(task_id)
        if not task:
            return False
        task.status = TaskStatus.IN_PROGRESS
        task.started_at = datetime.utcnow()
        logger.info(f"Task {task_id} started")
        return True
    
    def update_progress(self, task_id: str, progress: float,
                       message: Optional[str] = None) -> bool:
        task = self._tasks.get(task_id)
        if not task:
            return False
        task.progress = max(0.0, min(1.0, progress))
        for callback in self._progress_callbacks.get(task_id, []):
            try:
                callback(task_id, task.progress, message)
            except Exception as e:
                logger.error(f"Progress callback error: {e}")
        if task.on_progress:
            try:
                task.on_progress(task_id, task.progress, message)
            except Exception as e:
                logger.error(f"Task progress callback error: {e}")
        return True
    
    def complete_task(self, task_id: str, result: Any = None) -> bool:
        task = self._tasks.get(task_id)
        if not task:
            return False
        task.status = TaskStatus.COMPLETED
        task.completed_at = datetime.utcnow()
        task.progress = 1.0
        task.result = result
        for callback in self._completion_callbacks.get(task_id, []):
            try:
                callback(task_id, result)
            except Exception as e:
                logger.error(f"Completion callback error: {e}")
        if task.on_complete:
            try:
                task.on_complete(task_id, result)
            except Exception as e:
                logger.error(f"Task completion callback error: {e}")
        logger.info(f"Task {task_id} completed")
        return True
    
    def fail_task(self, task_id: str, error: str) -> bool:
        task = self._tasks.get(task_id)
        if not task:
            return False
        task.status = TaskStatus.FAILED
        task.completed_at = datetime.utcnow()
        task.error = error
        logger.error(f"Task {task_id} failed: {error}")
        return True
    
    def cancel_task(self, task_id: str) -> bool:
        task = self._tasks.get(task_id)
        if not task:
            return False
        if task.status in (TaskStatus.COMPLETED, TaskStatus.FAILED):
            logger.warning(f"Cannot cancel completed/failed task {task_id}")
            return False
        task.status = TaskStatus.CANCELLED
        task.completed_at = datetime.utcnow()
        logger.info(f"Task {task_id} cancelled")
        return True
    
    def create_subtask(self, parent_id: str, title: str, description: str,
                      task_type: str, input_data: Optional[Dict] = None,
                      priority: TaskPriority = TaskPriority.NORMAL) -> Optional[Task]:
        parent = self._tasks.get(parent_id)
        if not parent:
            return None
        subtask = self.create_task(
            title=title, description=description, task_type=task_type,
            creator_id=parent.creator_id, input_data=input_data,
            priority=priority, parent_id=parent_id
        )
        parent.subtasks.append(subtask)
        return subtask
    
    def get_task(self, task_id: str) -> Optional[Task]:
        return self._tasks.get(task_id)
    
    def get_tasks(self, status: Optional[TaskStatus] = None,
                 assignee_id: Optional[str] = None,
                 creator_id: Optional[str] = None) -> List[Task]:
        tasks = list(self._tasks.values())
        if status:
            tasks = [t for t in tasks if t.status == status]
        if assignee_id:
            tasks = [t for t in tasks if t.assignee_id == assignee_id]
        if creator_id:
            tasks = [t for t in tasks if t.creator_id == creator_id]
        return tasks
    
    def get_pending_tasks(self) -> List[Task]:
        return self.get_tasks(status=TaskStatus.PENDING)
    
    def register_progress_callback(self, task_id: str, callback: Callable):
        self._progress_callbacks[task_id].append(callback)
    
    def register_completion_callback(self, task_id: str, callback: Callable):
        self._completion_callbacks[task_id].append(callback)
    
    def get_statistics(self) -> Dict[str, Any]:
        status_counts = {}
        for status in TaskStatus:
            status_counts[status.value] = len([t for t in self._tasks.values() if t.status == status])
        return {
            "total_tasks": len(self._tasks),
            "status_breakdown": status_counts,
            "pending_tasks": len(self.get_pending_tasks())
        }
```


---

## 6. Inter-Agent Communication

### 6.1 Communication Patterns (`acp_communication.py`)

```python
"""ACP Communication Patterns - Advanced inter-agent communication utilities"""

import asyncio
from typing import Dict, List, Optional, Any, Callable, Set
from datetime import datetime
import logging
import uuid
from collections import defaultdict

logger = logging.getLogger("ACP.Communication")


class DirectMessaging:
    @staticmethod
    async def send(agent, recipient_id: str, action: str,
                  data: Dict[str, Any], priority=None):
        from acp_core import ACPMessage, MessageType, MessagePriority
        message = ACPMessage(
            message_type=MessageType.REQUEST,
            sender_id=agent.agent_id,
            recipient_id=recipient_id,
            action=action,
            data=data,
            priority=priority or MessagePriority.NORMAL
        )
        await agent.send_message(message)
    
    @staticmethod
    async def request_response(agent, recipient_id: str, action: str,
                               data: Dict[str, Any], timeout: float = 30.0):
        return await agent.send_request(recipient_id, action, data, timeout)


class BroadcastMessaging:
    @staticmethod
    async def broadcast(agent, action: str, data: Dict[str, Any]):
        await agent.broadcast(action, data)


class EventSystem:
    def __init__(self):
        self._subscribers: Dict[str, Set[str]] = {}
        self._agent_callbacks: Dict[str, Dict[str, Callable]] = {}
    
    def subscribe(self, agent_id: str, event_type: str, callback: Callable):
        if event_type not in self._subscribers:
            self._subscribers[event_type] = set()
        self._subscribers[event_type].add(agent_id)
        if agent_id not in self._agent_callbacks:
            self._agent_callbacks[agent_id] = {}
        self._agent_callbacks[agent_id][event_type] = callback
        logger.debug(f"Agent {agent_id} subscribed to {event_type}")
    
    def unsubscribe(self, agent_id: str, event_type: str):
        if event_type in self._subscribers:
            self._subscribers[event_type].discard(agent_id)
        if agent_id in self._agent_callbacks:
            self._agent_callbacks[agent_id].pop(event_type, None)
    
    async def publish(self, agent, event_type: str, data: Dict[str, Any]):
        from acp_core import ACPMessage, MessageType
        message = ACPMessage(
            message_type=MessageType.EVENT,
            sender_id=agent.agent_id,
            recipient_id="broadcast",
            action=event_type,
            data=data
        )
        await agent.send_message(message)
        logger.debug(f"Event {event_type} published by {agent.agent_id}")


class GroupMessaging:
    def __init__(self):
        self._groups: Dict[str, Set[str]] = {}
    
    def create_group(self, group_name: str):
        if group_name not in self._groups:
            self._groups[group_name] = set()
            logger.info(f"Group {group_name} created")
    
    def add_to_group(self, group_name: str, agent_id: str):
        if group_name not in self._groups:
            self.create_group(group_name)
        self._groups[group_name].add(agent_id)
        logger.debug(f"Agent {agent_id} added to group {group_name}")
    
    def remove_from_group(self, group_name: str, agent_id: str):
        if group_name in self._groups:
            self._groups[group_name].discard(agent_id)
    
    def get_group_members(self, group_name: str) -> Set[str]:
        return self._groups.get(group_name, set())
    
    async def send_to_group(self, agent, group_name: str, action: str, data: Dict[str, Any]):
        from acp_core import ACPMessage, MessageType
        members = self.get_group_members(group_name)
        for member_id in members:
            if member_id != agent.agent_id:
                message = ACPMessage(
                    message_type=MessageType.REQUEST,
                    sender_id=agent.agent_id,
                    recipient_id=member_id,
                    action=action,
                    data=data
                )
                await agent.send_message(message)


class MessageRouter:
    def __init__(self, registry):
        self.registry = registry
        self._routing_table: Dict[str, str] = {}
    
    def set_preferred_agent(self, capability: str, agent_id: str):
        self._routing_table[capability] = agent_id
    
    def find_best_agent(self, capability: str, min_health: float = 0.5) -> Optional[str]:
        if capability in self._routing_table:
            preferred_id = self._routing_table[capability]
            agent = self.registry.get_agent(preferred_id)
            if agent and agent.health_score >= min_health:
                return preferred_id
        agents = self.registry.find_by_capability(capability, min_health)
        if agents:
            return agents[0].agent_id
        return None
    
    async def route_message(self, sender, capability: str, action: str,
                           data: Dict[str, Any], timeout: float = 30.0):
        target_agent_id = self.find_best_agent(capability)
        if not target_agent_id:
            logger.error(f"No agent found for capability: {capability}")
            return None
        return await sender.send_request(target_agent_id, action, data, timeout)


class ConversationManager:
    def __init__(self):
        self._conversations: Dict[str, Dict[str, Any]] = {}
    
    def start_conversation(self, initiator_id: str, participant_ids: List[str]) -> str:
        conversation_id = str(uuid.uuid4())
        self._conversations[conversation_id] = {
            "id": conversation_id,
            "initiator": initiator_id,
            "participants": set(participant_ids),
            "messages": [],
            "started_at": datetime.utcnow(),
            "active": True
        }
        return conversation_id
    
    def add_message(self, conversation_id: str, sender_id: str, content: Dict[str, Any]):
        if conversation_id not in self._conversations:
            return False
        self._conversations[conversation_id]["messages"].append({
            "sender": sender_id,
            "content": content,
            "timestamp": datetime.utcnow().isoformat()
        })
        return True
    
    def get_conversation(self, conversation_id: str) -> Optional[Dict]:
        return self._conversations.get(conversation_id)
    
    def end_conversation(self, conversation_id: str):
        if conversation_id in self._conversations:
            self._conversations[conversation_id]["active"] = False
```

---

## 7. Specialized Agent Implementations

### 7.1 Code Agent (`agents/code_agent.py`)

```python
"""Code Agent - Specialized agent for code operations"""

import asyncio
from typing import Dict, List, Optional, Any
import logging

from acp_core import BaseAgent, ACPMessage, MessageType, AgentCapability

logger = logging.getLogger("ACP.CodeAgent")


class CodeAgent(BaseAgent):
    def __init__(self, agent_id: str, name: str = "CodeAgent", llm_client=None):
        capabilities = [
            AgentCapability(
                name="code_analysis",
                description="Analyze code for issues, patterns, and improvements",
                parameters={
                    "code": {"type": "string", "required": True},
                    "language": {"type": "string", "required": True},
                    "analysis_type": {"type": "string", "enum": ["static", "security", "performance", "style"]}
                },
                returns={"issues": {"type": "array"}, "suggestions": {"type": "array"}, "metrics": {"type": "object"}}
            ),
            AgentCapability(
                name="code_generation",
                description="Generate code from specifications",
                parameters={
                    "specification": {"type": "string", "required": True},
                    "language": {"type": "string", "required": True},
                    "context": {"type": "object"}
                },
                returns={"code": {"type": "string"}, "explanation": {"type": "string"}}
            ),
            AgentCapability(
                name="code_refactoring",
                description="Suggest and apply code refactoring",
                parameters={
                    "code": {"type": "string", "required": True},
                    "refactoring_type": {"type": "string", "enum": ["extract_method", "rename", "simplify", "optimize"]}
                },
                returns={"refactored_code": {"type": "string"}, "changes": {"type": "array"}}
            ),
            AgentCapability(
                name="code_review",
                description="Perform comprehensive code review",
                parameters={
                    "code": {"type": "string", "required": True},
                    "language": {"type": "string", "required": True},
                    "review_focus": {"type": "array"}
                },
                returns={"score": {"type": "number"}, "comments": {"type": "array"}, "recommendations": {"type": "array"}}
            )
        ]
        super().__init__(agent_id, name, capabilities)
        self.llm_client = llm_client
        self._code_cache: Dict[str, str] = {}
    
    async def initialize(self):
        logger.info(f"CodeAgent {self.name} initialized")
        self.register_handler("analyze_code", self._handle_analyze_code)
        self.register_handler("generate_code", self._handle_generate_code)
        self.register_handler("refactor_code", self._handle_refactor_code)
        self.register_handler("review_code", self._handle_review_code)
    
    async def shutdown(self):
        self._code_cache.clear()
        logger.info(f"CodeAgent {self.name} shutdown")
    
    async def _handle_analyze_code(self, message: ACPMessage) -> ACPMessage:
        data = message.data
        result = await self._analyze_code(
            data.get("code", ""), data.get("language", "python"), data.get("analysis_type", "static")
        )
        return ACPMessage(
            message_type=MessageType.RESPONSE, sender_id=self.agent_id,
            recipient_id=message.sender_id, correlation_id=message.message_id,
            action="code_analysis_result", data=result
        )
    
    async def _handle_generate_code(self, message: ACPMessage) -> ACPMessage:
        data = message.data
        result = await self._generate_code(
            data.get("specification", ""), data.get("language", "python"), data.get("context", {})
        )
        return ACPMessage(
            message_type=MessageType.RESPONSE, sender_id=self.agent_id,
            recipient_id=message.sender_id, correlation_id=message.message_id,
            action="code_generation_result", data=result
        )
    
    async def _handle_refactor_code(self, message: ACPMessage) -> ACPMessage:
        data = message.data
        result = await self._refactor_code(data.get("code", ""), data.get("refactoring_type", "simplify"))
        return ACPMessage(
            message_type=MessageType.RESPONSE, sender_id=self.agent_id,
            recipient_id=message.sender_id, correlation_id=message.message_id,
            action="code_refactoring_result", data=result
        )
    
    async def _handle_review_code(self, message: ACPMessage) -> ACPMessage:
        data = message.data
        result = await self._review_code(
            data.get("code", ""), data.get("language", "python"),
            data.get("review_focus", ["readability", "efficiency", "security"])
        )
        return ACPMessage(
            message_type=MessageType.RESPONSE, sender_id=self.agent_id,
            recipient_id=message.sender_id, correlation_id=message.message_id,
            action="code_review_result", data=result
        )
    
    async def _analyze_code(self, code: str, language: str, analysis_type: str) -> Dict[str, Any]:
        return {
            "issues": [],
            "suggestions": ["Consider adding type hints"],
            "metrics": {"complexity": 5, "lines": len(code.split(chr(10)))}
        }
    
    async def _generate_code(self, specification: str, language: str, context: Dict) -> Dict[str, Any]:
        return {
            "code": f"# Generated {language} code\n# TODO: Implement based on spec",
            "explanation": "Code generation placeholder"
        }
    
    async def _refactor_code(self, code: str, refactoring_type: str) -> Dict[str, Any]:
        return {"refactored_code": code, "changes": []}
    
    async def _review_code(self, code: str, language: str, review_focus: List[str]) -> Dict[str, Any]:
        return {"score": 8.5, "comments": ["Good structure"], "recommendations": ["Add more tests"]}
```

### 7.2 Research Agent (`agents/research_agent.py`)

```python
"""Research Agent - Specialized agent for information gathering"""

import asyncio
from typing import Dict, List, Optional, Any
import logging

from acp_core import BaseAgent, ACPMessage, MessageType, AgentCapability

logger = logging.getLogger("ACP.ResearchAgent")


class ResearchAgent(BaseAgent):
    def __init__(self, agent_id: str, name: str = "ResearchAgent", search_client=None, llm_client=None):
        capabilities = [
            AgentCapability(
                name="web_search",
                description="Search the web for information",
                parameters={"query": {"type": "string", "required": True}, "num_results": {"type": "integer", "default": 5}},
                returns={"results": {"type": "array"}, "total_found": {"type": "integer"}}
            ),
            AgentCapability(
                name="summarize",
                description="Summarize text or documents",
                parameters={"content": {"type": "string", "required": True}, "max_length": {"type": "integer", "default": 200}},
                returns={"summary": {"type": "string"}, "key_points": {"type": "array"}}
            ),
            AgentCapability(
                name="fact_check",
                description="Verify factual claims",
                parameters={"claim": {"type": "string", "required": True}, "sources": {"type": "array"}},
                returns={"verified": {"type": "boolean"}, "confidence": {"type": "number"}, "sources": {"type": "array"}}
            ),
            AgentCapability(
                name="document_lookup",
                description="Look up information in documentation",
                parameters={"topic": {"type": "string", "required": True}},
                returns={"found": {"type": "boolean"}, "content": {"type": "string"}, "references": {"type": "array"}}
            )
        ]
        super().__init__(agent_id, name, capabilities)
        self.search_client = search_client
        self.llm_client = llm_client
        self._knowledge_base: Dict[str, Any] = {}
    
    async def initialize(self):
        logger.info(f"ResearchAgent {self.name} initialized")
        self.register_handler("search", self._handle_search)
        self.register_handler("summarize", self._handle_summarize)
        self.register_handler("fact_check", self._handle_fact_check)
        self.register_handler("lookup", self._handle_lookup)
    
    async def shutdown(self):
        self._knowledge_base.clear()
        logger.info(f"ResearchAgent {self.name} shutdown")
    
    async def _handle_search(self, message: ACPMessage) -> ACPMessage:
        data = message.data
        results = await self._web_search(data.get("query", ""), data.get("num_results", 5), data.get("filters", {}))
        return ACPMessage(
            message_type=MessageType.RESPONSE, sender_id=self.agent_id,
            recipient_id=message.sender_id, correlation_id=message.message_id,
            action="search_results", data=results
        )
    
    async def _handle_summarize(self, message: ACPMessage) -> ACPMessage:
        data = message.data
        summary = await self._summarize(data.get("content", ""), data.get("max_length", 200), data.get("style", "brief"))
        return ACPMessage(
            message_type=MessageType.RESPONSE, sender_id=self.agent_id,
            recipient_id=message.sender_id, correlation_id=message.message_id,
            action="summary", data=summary
        )
    
    async def _handle_fact_check(self, message: ACPMessage) -> ACPMessage:
        data = message.data
        result = await self._fact_check(data.get("claim", ""), data.get("sources", []))
        return ACPMessage(
            message_type=MessageType.RESPONSE, sender_id=self.agent_id,
            recipient_id=message.sender_id, correlation_id=message.message_id,
            action="fact_check_result", data=result
        )
    
    async def _handle_lookup(self, message: ACPMessage) -> ACPMessage:
        data = message.data
        result = await self._document_lookup(data.get("topic", ""), data.get("source"), data.get("version"))
        return ACPMessage(
            message_type=MessageType.RESPONSE, sender_id=self.agent_id,
            recipient_id=message.sender_id, correlation_id=message.message_id,
            action="lookup_result", data=result
        )
    
    async def _web_search(self, query: str, num_results: int, filters: Dict) -> Dict[str, Any]:
        return {
            "results": [
                {"title": "Result 1", "url": "http://example.com/1", "snippet": "..."},
                {"title": "Result 2", "url": "http://example.com/2", "snippet": "..."}
            ],
            "total_found": 2
        }
    
    async def _summarize(self, content: str, max_length: int, style: str) -> Dict[str, Any]:
        return {
            "summary": content[:max_length] + "..." if len(content) > max_length else content,
            "key_points": ["Point 1", "Point 2"]
        }
    
    async def _fact_check(self, claim: str, sources: List[str]) -> Dict[str, Any]:
        return {"verified": True, "confidence": 0.85, "sources": sources or ["source1", "source2"]}
    
    async def _document_lookup(self, topic: str, source: Optional[str], version: Optional[str]) -> Dict[str, Any]:
        return {"found": True, "content": f"Documentation for {topic}", "references": ["ref1", "ref2"]}
```

### 7.3 Review Agent (`agents/review_agent.py`)

```python
"""Review Agent - Specialized agent for quality assurance"""

import asyncio
from typing import Dict, List, Optional, Any
import logging

from acp_core import BaseAgent, ACPMessage, MessageType, AgentCapability

logger = logging.getLogger("ACP.ReviewAgent")


class ReviewAgent(BaseAgent):
    def __init__(self, agent_id: str, name: str = "ReviewAgent", llm_client=None):
        capabilities = [
            AgentCapability(
                name="quality_review",
                description="Perform comprehensive quality review",
                parameters={"content": {"type": "string", "required": True}, "content_type": {"type": "string"}},
                returns={"score": {"type": "number"}, "issues": {"type": "array"}, "recommendations": {"type": "array"}}
            ),
            AgentCapability(
                name="test_validation",
                description="Validate test coverage and quality",
                parameters={"code": {"type": "string", "required": True}, "tests": {"type": "string", "required": True}},
                returns={"coverage_score": {"type": "number"}, "missing_tests": {"type": "array"}, "suggestions": {"type": "array"}}
            ),
            AgentCapability(
                name="compliance_check",
                description="Check compliance with standards",
                parameters={"content": {"type": "string", "required": True}, "standard": {"type": "string", "required": True}},
                returns={"compliant": {"type": "boolean"}, "violations": {"type": "array"}, "suggestions": {"type": "array"}}
            ),
            AgentCapability(
                name="security_audit",
                description="Perform security audit",
                parameters={"code": {"type": "string", "required": True}, "language": {"type": "string", "required": True}},
                returns={"risk_score": {"type": "number"}, "vulnerabilities": {"type": "array"}, "recommendations": {"type": "array"}}
            )
        ]
        super().__init__(agent_id, name, capabilities)
        self.llm_client = llm_client
        self._review_history: List[Dict] = []
    
    async def initialize(self):
        logger.info(f"ReviewAgent {self.name} initialized")
        self.register_handler("quality_review", self._handle_quality_review)
        self.register_handler("validate_tests", self._handle_test_validation)
        self.register_handler("compliance_check", self._handle_compliance_check)
        self.register_handler("security_audit", self._handle_security_audit)
    
    async def shutdown(self):
        self._review_history.clear()
        logger.info(f"ReviewAgent {self.name} shutdown")
    
    async def _handle_quality_review(self, message: ACPMessage) -> ACPMessage:
        data = message.data
        result = await self._quality_review(data.get("content", ""), data.get("content_type", "code"), data.get("criteria", []))
        self._review_history.append({"type": "quality", "result": result})
        return ACPMessage(
            message_type=MessageType.RESPONSE, sender_id=self.agent_id,
            recipient_id=message.sender_id, correlation_id=message.message_id,
            action="quality_review_result", data=result
        )
    
    async def _handle_test_validation(self, message: ACPMessage) -> ACPMessage:
        data = message.data
        result = await self._validate_tests(data.get("code", ""), data.get("tests", ""), data.get("language", "python"))
        return ACPMessage(
            message_type=MessageType.RESPONSE, sender_id=self.agent_id,
            recipient_id=message.sender_id, correlation_id=message.message_id,
            action="test_validation_result", data=result
        )
    
    async def _handle_compliance_check(self, message: ACPMessage) -> ACPMessage:
        data = message.data
        result = await self._compliance_check(data.get("content", ""), data.get("standard", ""), data.get("rules", []))
        return ACPMessage(
            message_type=MessageType.RESPONSE, sender_id=self.agent_id,
            recipient_id=message.sender_id, correlation_id=message.message_id,
            action="compliance_result", data=result
        )
    
    async def _handle_security_audit(self, message: ACPMessage) -> ACPMessage:
        data = message.data
        result = await self._security_audit(data.get("code", ""), data.get("language", "python"), data.get("audit_level", "standard"))
        return ACPMessage(
            message_type=MessageType.RESPONSE, sender_id=self.agent_id,
            recipient_id=message.sender_id, correlation_id=message.message_id,
            action="security_audit_result", data=result
        )
    
    async def _quality_review(self, content: str, content_type: str, criteria: List[str]) -> Dict[str, Any]:
        return {"score": 8.5, "issues": [], "recommendations": ["Consider adding more comments"]}
    
    async def _validate_tests(self, code: str, tests: str, language: str) -> Dict[str, Any]:
        return {"coverage_score": 0.75, "missing_tests": ["test_edge_case_1"], "suggestions": ["Add test for error handling"]}
    
    async def _compliance_check(self, content: str, standard: str, rules: List[str]) -> Dict[str, Any]:
        return {"compliant": True, "violations": [], "suggestions": []}
    
    async def _security_audit(self, code: str, language: str, audit_level: str) -> Dict[str, Any]:
        return {"risk_score": 2.5, "vulnerabilities": [], "recommendations": ["Use parameterized queries"]}
```


---

## 8. Coordination Strategies

### 8.1 Coordination Patterns (`acp_coordination.py`)

```python
"""ACP Coordination Strategies - Multi-agent coordination patterns"""

import asyncio
from abc import ABC, abstractmethod
from typing import Dict, List, Optional, Any
from enum import Enum
import logging
import uuid
from collections import defaultdict

logger = logging.getLogger("ACP.Coordination")


class CoordinationPattern(Enum):
    MASTER_WORKER = "master_worker"
    PEER_TO_PEER = "peer_to_peer"
    PIPELINE = "pipeline"
    VOTING = "voting"


class BaseCoordinator(ABC):
    def __init__(self, coordinator_id: str, task_manager):
        self.coordinator_id = coordinator_id
        self.task_manager = task_manager
        self._agents: Dict[str, Any] = {}
        self._running = False
    
    def add_agent(self, agent):
        self._agents[agent.agent_id] = agent
        logger.info(f"Agent {agent.name} added to coordination")
    
    def remove_agent(self, agent_id: str):
        if agent_id in self._agents:
            agent = self._agents.pop(agent_id)
            logger.info(f"Agent {agent.name} removed from coordination")
    
    @abstractmethod
    async def execute(self, task) -> Any:
        pass
    
    @abstractmethod
    async def initialize(self):
        pass
    
    @abstractmethod
    async def shutdown(self):
        pass


class MasterWorkerCoordinator(BaseCoordinator):
    """Master-Worker Coordination Pattern - distributes tasks to workers and aggregates results."""
    
    def __init__(self, coordinator_id: str, task_manager, master_agent):
        super().__init__(coordinator_id, task_manager)
        self.master = master_agent
        self.workers: Dict[str, Any] = {}
        self._worker_load: Dict[str, int] = {}
    
    def add_worker(self, worker):
        self.workers[worker.agent_id] = worker
        self._worker_load[worker.agent_id] = 0
        logger.info(f"Worker {worker.name} added")
    
    def remove_worker(self, worker_id: str):
        if worker_id in self.workers:
            worker = self.workers.pop(worker_id)
            self._worker_load.pop(worker_id, None)
            logger.info(f"Worker {worker.name} removed")
    
    def get_least_loaded_worker(self) -> Optional[str]:
        if not self._worker_load:
            return None
        return min(self._worker_load.items(), key=lambda x: x[1])[0]
    
    async def distribute_task(self, task) -> Optional[str]:
        worker_id = self.get_least_loaded_worker()
        if not worker_id:
            logger.error("No workers available")
            return None
        self._worker_load[worker_id] += 1
        self.task_manager.assign_task(task.task_id, worker_id)
        response = await self.master.send_request(
            worker_id, "execute_task",
            {"task_id": task.task_id, "task_type": task.task_type, "input_data": task.input_data}
        )
        return response
    
    async def execute(self, task) -> Any:
        subtasks = task.input_data.get("subtasks", [])
        if not subtasks:
            return await self.distribute_task(task)
        subtask_objects = []
        for subtask_data in subtasks:
            subtask = self.task_manager.create_subtask(
                parent_id=task.task_id,
                title=subtask_data.get("title", "Subtask"),
                description=subtask_data.get("description", ""),
                task_type=subtask_data.get("task_type", task.task_type),
                input_data=subtask_data.get("input_data", {})
            )
            subtask_objects.append(subtask)
        results = await asyncio.gather(*[self.distribute_task(st) for st in subtask_objects], return_exceptions=True)
        aggregated = self._aggregate_results(results)
        self.task_manager.complete_task(task.task_id, aggregated)
        return aggregated
    
    def _aggregate_results(self, results: List[Any]) -> Dict[str, Any]:
        successful = [r for r in results if not isinstance(r, Exception)]
        failed = [r for r in results if isinstance(r, Exception)]
        return {
            "successful_count": len(successful),
            "failed_count": len(failed),
            "results": successful,
            "errors": [str(e) for e in failed]
        }
    
    async def initialize(self):
        self._running = True
        logger.info("Master-Worker coordinator initialized")
    
    async def shutdown(self):
        self._running = False
        logger.info("Master-Worker coordinator shutdown")


class PipelineCoordinator(BaseCoordinator):
    """Pipeline Coordination Pattern - tasks flow through a sequence of processing stages."""
    
    def __init__(self, coordinator_id: str, task_manager):
        super().__init__(coordinator_id, task_manager)
        self.stages: List[Dict[str, Any]] = []
        self._stage_agents: Dict[int, List[str]] = {}
    
    def add_stage(self, name: str, capability: str, agents: List[str], optional: bool = False):
        stage = {
            "name": name, "capability": capability, "agents": agents,
            "optional": optional, "index": len(self.stages)
        }
        self.stages.append(stage)
        self._stage_agents[len(self.stages) - 1] = agents
        logger.info(f"Stage '{name}' added at position {stage['index']}")
    
    async def execute(self, task) -> Any:
        data = task.input_data
        for stage in self.stages:
            logger.info(f"Processing stage: {stage['name']}")
            agent_id = stage["agents"][0]
            if agent_id not in self._agents:
                if stage["optional"]:
                    logger.warning(f"Optional stage {stage['name']} skipped")
                    continue
                else:
                    raise RuntimeError(f"Agent {agent_id} not available for stage {stage['name']}")
            agent = self._agents[agent_id]
            response = await agent.send_request(agent_id, stage["capability"], {"input": data, "stage": stage["name"]})
            if not response:
                raise RuntimeError(f"Stage {stage['name']} failed")
            data = response.data.get("output", data)
        return data
    
    async def initialize(self):
        self._running = True
        logger.info("Pipeline coordinator initialized")
    
    async def shutdown(self):
        self._running = False
        logger.info("Pipeline coordinator shutdown")


class VotingCoordinator(BaseCoordinator):
    """Voting/Consensus Coordination Pattern - multiple agents vote on a decision."""
    
    def __init__(self, coordinator_id: str, task_manager, min_agreement: float = 0.5):
        super().__init__(coordinator_id, task_manager)
        self.voters: Dict[str, Any] = {}
        self.min_agreement = min_agreement
    
    def add_voter(self, voter):
        self.voters[voter.agent_id] = voter
        logger.info(f"Voter {voter.name} added")
    
    async def execute(self, task) -> Any:
        proposal = task.input_data.get("proposal", {})
        voting_criteria = task.input_data.get("criteria", [])
        votes = await self._collect_votes(proposal, voting_criteria)
        result = self._aggregate_votes(votes)
        return result
    
    async def _collect_votes(self, proposal: Dict, criteria: List[str]) -> List[Dict]:
        vote_tasks = []
        for voter_id, voter in self.voters.items():
            task = voter.send_request(voter_id, "vote", {"proposal": proposal, "criteria": criteria})
            vote_tasks.append(task)
        responses = await asyncio.gather(*vote_tasks, return_exceptions=True)
        votes = []
        for response in responses:
            if isinstance(response, Exception):
                logger.error(f"Vote collection failed: {response}")
            elif response:
                votes.append(response.data)
        return votes
    
    def _aggregate_votes(self, votes: List[Dict]) -> Dict[str, Any]:
        if not votes:
            return {"consensus": False, "reason": "No votes collected"}
        vote_counts: Dict[str, int] = {}
        for vote in votes:
            decision = vote.get("decision", "abstain")
            vote_counts[decision] = vote_counts.get(decision, 0) + 1
        total_votes = len(votes)
        majority_decision = max(vote_counts.items(), key=lambda x: x[1])
        majority_ratio = majority_decision[1] / total_votes
        consensus_reached = majority_ratio >= self.min_agreement
        return {
            "consensus": consensus_reached,
            "decision": majority_decision[0],
            "agreement_ratio": majority_ratio,
            "total_votes": total_votes,
            "vote_breakdown": vote_counts,
            "voter_count": len(self.voters)
        }
    
    async def initialize(self):
        self._running = True
        logger.info("Voting coordinator initialized")
    
    async def shutdown(self):
        self._running = False
        logger.info("Voting coordinator shutdown")


class PeerToPeerCoordinator(BaseCoordinator):
    """Peer-to-Peer Coordination Pattern - agents collaborate directly without central coordination."""
    
    def __init__(self, coordinator_id: str, task_manager):
        super().__init__(coordinator_id, task_manager)
        self._collaboration_protocols: Dict[str, Any] = {}
    
    async def initiate_collaboration(self, initiator_id: str, participant_ids: List[str], task_data: Dict) -> Dict[str, Any]:
        collaboration_id = str(uuid.uuid4())
        for participant_id in participant_ids:
            if participant_id in self._agents:
                participant = self._agents[participant_id]
                await participant.send_request(participant_id, "collaboration_invite", {
                    "collaboration_id": collaboration_id,
                    "initiator": initiator_id,
                    "participants": participant_ids,
                    "task": task_data
                })
        self._collaboration_protocols[collaboration_id] = {
            "id": collaboration_id, "initiator": initiator_id,
            "participants": participant_ids, "status": "active"
        }
        return {"collaboration_id": collaboration_id, "status": "initiated"}
    
    async def execute(self, task) -> Any:
        collaboration_data = task.input_data.get("collaboration", {})
        return await self.initiate_collaboration(
            collaboration_data.get("initiator"),
            collaboration_data.get("participants", []),
            collaboration_data.get("task", {})
        )
    
    async def initialize(self):
        self._running = True
        logger.info("Peer-to-Peer coordinator initialized")
    
    async def shutdown(self):
        self._running = False
        logger.info("Peer-to-Peer coordinator shutdown")
```

---

## 9. Configuration Files

### 9.1 Agent Configuration (`config/agents.yaml`)

```yaml
# ACP Multi-Agent System Configuration

system:
  name: "Local LLM Multi-Agent System"
  version: "1.0.0"
  log_level: "INFO"

communication:
  channel_type: "asyncio"
  message_ttl: 300
  max_queue_size: 10000
  enable_signatures: false

registry:
  heartbeat_timeout: 60
  heartbeat_interval: 30

task_manager:
  max_concurrent_tasks: 100
  default_task_timeout: 300
  enable_retries: true
  max_retries: 3

agents:
  - id: "orchestrator-001"
    name: "MainOrchestrator"
    type: "orchestrator"
    enabled: true
    capabilities:
      - task_routing
      - delegation
      - result_aggregation
    config:
      coordination_pattern: "master_worker"
      max_workers: 10

  - id: "code-001"
    name: "CodeAgent"
    type: "specialized"
    enabled: true
    capabilities:
      - code_analysis
      - code_generation
      - code_refactoring
      - code_review
    config:
      llm_model: "codellama:13b"
      max_code_length: 10000
      supported_languages:
        - python
        - javascript
        - typescript
        - java
        - cpp

  - id: "research-001"
    name: "ResearchAgent"
    type: "specialized"
    enabled: true
    capabilities:
      - web_search
      - summarize
      - fact_check
      - document_lookup
    config:
      llm_model: "llama2:7b"
      search_provider: "duckduckgo"
      max_results: 10
      cache_ttl: 3600

  - id: "review-001"
    name: "ReviewAgent"
    type: "specialized"
    enabled: true
    capabilities:
      - quality_review
      - test_validation
      - compliance_check
      - security_audit
    config:
      llm_model: "llama2:7b"
      review_criteria:
        - readability
        - efficiency
        - security
        - maintainability

  - id: "doc-001"
    name: "DocAgent"
    type: "support"
    enabled: true
    capabilities:
      - doc_generation
      - api_doc_creation
      - tutorial_creation
    config:
      llm_model: "llama2:7b"
      output_formats:
        - markdown
        - html
        - pdf

coordination:
  default_pattern: "master_worker"
  patterns:
    master_worker:
      enabled: true
      load_balancing: "round_robin"
      retry_failed_tasks: true
    
    pipeline:
      enabled: true
      stages:
        - name: "extract"
          capability: "data_extraction"
        - name: "transform"
          capability: "data_transformation"
        - name: "validate"
          capability: "data_validation"
        - name: "load"
          capability: "data_loading"
    
    voting:
      enabled: true
      min_agreement: 0.6
      voting_timeout: 60
    
    peer_to_peer:
      enabled: true
      max_participants: 10

network:
  enabled: false
  host: "localhost"
  port: 8080
  transport: "websocket"
  ssl_enabled: false

llm:
  default_provider: "ollama"
  providers:
    ollama:
      base_url: "http://localhost:11434"
      default_model: "llama2:7b"
      timeout: 120
    
    openai:
      api_key: null
      base_url: "https://api.openai.com/v1"
      default_model: "gpt-3.5-turbo"

monitoring:
  enabled: true
  metrics_interval: 60
  export_format: "prometheus"

security:
  enable_authentication: false
  enable_encryption: false
  allowed_agents: []
```

### 9.2 Environment Configuration (`config/.env`)

```bash
# ACP Multi-Agent System Environment Variables

# System
ACP_LOG_LEVEL=INFO
ACP_DEBUG=false

# LLM Configuration
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_DEFAULT_MODEL=llama2:7b
OPENAI_API_KEY=

# Communication
ACP_ENABLE_SIGNATURES=false
ACP_SECRET_KEY=

# Registry
ACP_HEARTBEAT_TIMEOUT=60
ACP_HEARTBEAT_INTERVAL=30

# Network
ACP_NETWORK_ENABLED=false
ACP_HOST=localhost
ACP_PORT=8080
ACP_SSL_ENABLED=false

# Monitoring
ACP_MONITORING_ENABLED=true
ACP_METRICS_PORT=9090
```

---

## 10. Usage Examples

### 10.1 Basic Setup Example

```python
"""Example: Basic ACP Multi-Agent System Setup"""

import asyncio
from acp_core import ACPChannel, BaseAgent, AgentCapability
from acp_registry import ACPRegistry
from acp_task_manager import TaskManager
from acp_coordination import MasterWorkerCoordinator

async def main():
    # Initialize core components
    channel = ACPChannel()
    registry = ACPRegistry()
    task_manager = TaskManager()
    
    await channel.start()
    await registry.start()
    
    # Create agents
    from agents.code_agent import CodeAgent
    from agents.research_agent import ResearchAgent
    from agents.review_agent import ReviewAgent
    
    code_agent = CodeAgent("code-001", "CodeAgent")
    research_agent = ResearchAgent("research-001", "ResearchAgent")
    review_agent = ReviewAgent("review-001", "ReviewAgent")
    
    # Initialize and connect agents
    for agent in [code_agent, research_agent, review_agent]:
        await agent.initialize()
        await agent.connect(channel)
    
    # Register agents
    from acp_registration_client import RegistrationClient
    for agent in [code_agent, research_agent, review_agent]:
        reg_client = RegistrationClient(agent, registry)
        await reg_client.register()
    
    # Create coordinator
    orchestrator = BaseAgent("orch-001", "Orchestrator", [])
    await orchestrator.connect(channel)
    
    coordinator = MasterWorkerCoordinator("coord-001", task_manager, orchestrator)
    for agent in [code_agent, research_agent, review_agent]:
        coordinator.add_worker(agent)
    
    await coordinator.initialize()
    
    # Create and execute task
    task = task_manager.create_task(
        title="Analyze Code Quality",
        description="Analyze and review Python code",
        task_type="code_analysis",
        creator_id=orchestrator.agent_id,
        input_data={"code": "def example(): pass", "language": "python"}
    )
    
    result = await coordinator.execute(task)
    print(f"Task result: {result}")
    
    # Cleanup
    await coordinator.shutdown()
    for agent in [code_agent, research_agent, review_agent, orchestrator]:
        await agent.disconnect()
    await registry.stop()
    await channel.stop()

if __name__ == "__main__":
    asyncio.run(main())
```

### 10.2 Pipeline Processing Example

```python
"""Example: Pipeline Processing with Multiple Stages"""

import asyncio
from acp_core import ACPChannel, BaseAgent, AgentCapability
from acp_registry import ACPRegistry
from acp_task_manager import TaskManager
from acp_coordination import PipelineCoordinator

async def pipeline_example():
    channel = ACPChannel()
    registry = ACPRegistry()
    task_manager = TaskManager()
    
    await channel.start()
    await registry.start()
    
    # Create agents for each stage
    extract_agent = BaseAgent("extract-001", "ExtractAgent", [
        AgentCapability("data_extraction", "Extract data from various sources")
    ])
    transform_agent = BaseAgent("transform-001", "TransformAgent", [
        AgentCapability("data_transformation", "Transform and process data")
    ])
    validate_agent = BaseAgent("validate-001", "ValidateAgent", [
        AgentCapability("data_validation", "Validate processed data")
    ])
    load_agent = BaseAgent("load-001", "LoadAgent", [
        AgentCapability("data_loading", "Load data to destination")
    ])
    
    for agent in [extract_agent, transform_agent, validate_agent, load_agent]:
        await agent.connect(channel)
    
    # Create pipeline
    pipeline = PipelineCoordinator("pipeline-001", task_manager)
    pipeline.add_stage("extract", "data_extraction", ["extract-001"])
    pipeline.add_stage("transform", "data_transformation", ["transform-001"])
    pipeline.add_stage("validate", "data_validation", ["validate-001"])
    pipeline.add_stage("load", "data_loading", ["load-001"])
    
    for agent in [extract_agent, transform_agent, validate_agent, load_agent]:
        pipeline.add_agent(agent)
    
    await pipeline.initialize()
    
    # Execute pipeline task
    task = task_manager.create_task(
        title="Data Processing Pipeline",
        description="Process data through ETL pipeline",
        task_type="pipeline",
        creator_id="user-001",
        input_data={"source": "input_data.csv", "format": "csv"}
    )
    
    result = await pipeline.execute(task)
    print(f"Pipeline result: {result}")
    
    await pipeline.shutdown()
    await registry.stop()
    await channel.stop()

if __name__ == "__main__":
    asyncio.run(pipeline_example())
```

### 10.3 Voting/Consensus Example

```python
"""Example: Voting/Consensus Decision Making"""

import asyncio
from acp_core import ACPChannel, BaseAgent, AgentCapability
from acp_registry import ACPRegistry
from acp_task_manager import TaskManager
from acp_coordination import VotingCoordinator

async def voting_example():
    channel = ACPChannel()
    registry = ACPRegistry()
    task_manager = TaskManager()
    
    await channel.start()
    await registry.start()
    
    # Create voting agents
    security_expert = BaseAgent("security-001", "SecurityExpert", [
        AgentCapability("security_review", "Security-focused code review")
    ])
    performance_expert = BaseAgent("perf-001", "PerformanceExpert", [
        AgentCapability("performance_review", "Performance-focused code review")
    ])
    maintainability_expert = BaseAgent("maint-001", "MaintainabilityExpert", [
        AgentCapability("maintainability_review", "Maintainability-focused code review")
    ])
    
    for agent in [security_expert, performance_expert, maintainability_expert]:
        await agent.connect(channel)
    
    # Create voting coordinator
    voting = VotingCoordinator("voting-001", task_manager, min_agreement=0.6)
    for agent in [security_expert, performance_expert, maintainability_expert]:
        voting.add_voter(agent)
    
    await voting.initialize()
    
    # Create voting task
    task = task_manager.create_task(
        title="Code Review Voting",
        description="Vote on whether to approve code changes",
        task_type="voting",
        creator_id="user-001",
        input_data={
            "proposal": {"code_changes": "...", "description": "Refactor authentication module"},
            "criteria": ["security", "performance", "maintainability"]
        }
    )
    
    result = await voting.execute(task)
    print(f"Voting result: {result}")
    
    await voting.shutdown()
    await registry.stop()
    await channel.stop()

if __name__ == "__main__":
    asyncio.run(voting_example())
```

---

## File Structure

```
acp_system/
├── __init__.py
├── acp_core.py              # Core ACP protocol classes
├── acp_registry.py          # Agent registry service
├── acp_registration_client.py  # Registration client
├── acp_task_manager.py      # Task management
├── acp_communication.py     # Communication patterns
├── acp_coordination.py      # Coordination strategies
├── agents/
│   ├── __init__.py
│   ├── code_agent.py        # Code specialist agent
│   ├── research_agent.py    # Research agent
│   ├── review_agent.py      # Review/QA agent
│   └── doc_agent.py         # Documentation agent
├── config/
│   ├── agents.yaml          # Agent configuration
│   └── .env                 # Environment variables
└── examples/
    ├── basic_setup.py       # Basic setup example
    ├── pipeline_example.py  # Pipeline processing
    └── voting_example.py    # Voting/consensus
```

---

## Summary

This implementation provides a complete ACP (Agent Communication Protocol) multi-agent system with:

1. **Core Protocol**: Message-based communication with standardized formats
2. **Agent Types**: Specialized agents for different tasks (Code, Research, Review, Doc)
3. **Registration System**: Service discovery and health monitoring
4. **Task Management**: Full task lifecycle management (create, assign, track, complete)
5. **Communication Patterns**: Direct, broadcast, pub/sub, and group messaging
6. **Coordination Strategies**: Master-worker, pipeline, voting, and P2P patterns
7. **Configuration**: YAML-based configuration with environment variable support

The system is designed to be:
- **Modular**: Each component can be used independently
- **Extensible**: Easy to add new agent types and capabilities
- **Scalable**: Supports multiple coordination patterns
- **Local-first**: Optimized for local LLM deployments

---

*Generated for Phase 6: ACP Multi-Agent Communication System*
