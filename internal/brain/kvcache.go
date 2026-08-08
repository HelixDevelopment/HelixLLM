// Package brain provides LLM provider routing and request handling.
//
// kvcache.go implements a key-value cache for persisting conversation context
// (message histories) across sessions. Two backends are provided:
//
//   - RedisKVCache  -- backed by Redis via go-redis/v9; preferred in production.
//   - MemoryKVCache -- pure in-memory fallback with TTL-based expiry; used when
//     Redis is unavailable or in unit tests.
//
// Both implement the KVCacher interface so callers can swap backends
// transparently.
package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// ---------------------------------------------------------------------------
// Interface
// ---------------------------------------------------------------------------

// KVCacher is the common contract for context-persistence caches.
type KVCacher interface {
	Store(ctx context.Context, sessionID string, messages []types.InternalMessage) error
	Retrieve(ctx context.Context, sessionID string) ([]types.InternalMessage, error)
	Invalidate(ctx context.Context, sessionID string) error
	Stats(ctx context.Context) (*CacheStats, error)
	Available() bool
	Close() error
}

// CacheStats contains high-level cache metrics.
type CacheStats struct {
	TotalSessions int64  `json:"total_sessions"`
	MemoryUsed    string `json:"memory_used"`
}

// ---------------------------------------------------------------------------
// KVCacheConfig
// ---------------------------------------------------------------------------

// KVCacheConfig holds connection and behaviour parameters for the KV cache.
type KVCacheConfig struct {
	RedisAddr     string        // "host:port"
	RedisPassword string
	DefaultTTL    time.Duration // 0 means 1 hour
	KeyPrefix     string        // "" means "helixllm:kv:"
}

func (c *KVCacheConfig) ttl() time.Duration {
	if c.DefaultTTL <= 0 {
		return time.Hour
	}
	return c.DefaultTTL
}

func (c *KVCacheConfig) prefix() string {
	if c.KeyPrefix == "" {
		return "helixllm:kv:"
	}
	return c.KeyPrefix
}

// ---------------------------------------------------------------------------
// RedisKVCache
// ---------------------------------------------------------------------------

// RedisKVCache persists session message histories in Redis.
type RedisKVCache struct {
	client    redis.UniversalClient
	config    KVCacheConfig
	mu        sync.RWMutex
	available bool
}

// NewKVCache creates a Redis-backed KV cache. If Redis is unreachable the
// cache is marked unavailable and all Store/Retrieve calls return early
// (graceful degradation).
func NewKVCache(cfg KVCacheConfig) *RedisKVCache {
	c := &RedisKVCache{config: cfg}

	c.client = redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := c.client.Ping(ctx).Err(); err != nil {
		c.mu.Lock()
		c.available = false
		c.mu.Unlock()
	} else {
		c.mu.Lock()
		c.available = true
		c.mu.Unlock()
	}
	return c
}

// Store persists a session's message history in Redis with the configured TTL.
func (c *RedisKVCache) Store(
	ctx context.Context,
	sessionID string,
	messages []types.InternalMessage,
) error {
	if !c.Available() {
		return fmt.Errorf("kvcache: redis unavailable")
	}
	data, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("kvcache: marshal: %w", err)
	}
	key := c.config.prefix() + sessionID
	return c.client.Set(ctx, key, data, c.config.ttl()).Err()
}

// Retrieve loads a session's message history from Redis. Returns nil, nil when
// the key does not exist.
func (c *RedisKVCache) Retrieve(
	ctx context.Context,
	sessionID string,
) ([]types.InternalMessage, error) {
	if !c.Available() {
		return nil, fmt.Errorf("kvcache: redis unavailable")
	}
	key := c.config.prefix() + sessionID
	data, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("kvcache: get: %w", err)
	}
	var msgs []types.InternalMessage
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, fmt.Errorf("kvcache: unmarshal: %w", err)
	}
	return msgs, nil
}

// Invalidate removes a session's cached history.
func (c *RedisKVCache) Invalidate(ctx context.Context, sessionID string) error {
	if !c.Available() {
		return fmt.Errorf("kvcache: redis unavailable")
	}
	key := c.config.prefix() + sessionID
	return c.client.Del(ctx, key).Err()
}

// Stats returns the number of cached sessions and Redis memory usage.
func (c *RedisKVCache) Stats(ctx context.Context) (*CacheStats, error) {
	if !c.Available() {
		return nil, fmt.Errorf("kvcache: redis unavailable")
	}

	// Count keys matching the prefix via SCAN (non-blocking).
	var total int64
	iter := c.client.Scan(ctx, 0, c.config.prefix()+"*", 100).Iterator()
	for iter.Next(ctx) {
		total++
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("kvcache: scan: %w", err)
	}

	// Memory info.
	info, err := c.client.Info(ctx, "memory").Result()
	if err != nil {
		return nil, fmt.Errorf("kvcache: info: %w", err)
	}
	memUsed := parseRedisMemory(info)

	return &CacheStats{
		TotalSessions: total,
		MemoryUsed:    memUsed,
	}, nil
}

// Available reports whether the Redis backend was reachable AT CONSTRUCTION
// TIME. It is a cached boolean set once by NewKVCache and never refreshed —
// it is NOT a liveness probe. Callers that need to know whether Redis is
// reachable NOW must use Ping.
func (c *RedisKVCache) Available() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.available
}

// Ping performs a REAL Redis round-trip (a RESP PING command) bounded by ctx,
// and returns the transport/protocol error verbatim on failure.
//
// This exists because Available() answers a different question: it reports the
// reachability observed once during NewKVCache and is never refreshed, so it
// cannot distinguish "Redis is up" from "Redis was up when this process
// started". A health endpoint that reported Available() would be publishing
// startup metadata as if it were current liveness — the absence-of-error
// verdict this package's callers must never emit.
//
// Ping is deliberately side-effect-free: it does NOT update the cached
// `available` flag. Flipping that flag would change Store/Retrieve short-circuit
// behaviour as a side effect of an observability call, which belongs to a
// separate, independently-reviewed change rather than to a probe.
func (c *RedisKVCache) Ping(ctx context.Context) error {
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("kvcache: redis ping %s: %w", c.config.RedisAddr, err)
	}
	return nil
}

// Close shuts down the Redis connection.
func (c *RedisKVCache) Close() error {
	return c.client.Close()
}

// parseRedisMemory extracts used_memory_human from the INFO memory output.
func parseRedisMemory(info string) string {
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "used_memory_human:") {
			return strings.TrimPrefix(line, "used_memory_human:")
		}
	}
	return "unknown"
}

// ---------------------------------------------------------------------------
// MemoryKVCache (in-memory fallback)
// ---------------------------------------------------------------------------

type cacheEntry struct {
	data      []byte
	expiresAt time.Time
}

// MemoryKVCache is a pure in-memory KV cache with TTL-based expiry. It is used
// when Redis is unavailable and for testing.
type MemoryKVCache struct {
	mu   sync.RWMutex
	data map[string]cacheEntry
	ttl  time.Duration
}

// NewMemoryKVCache creates an in-memory cache with the given TTL.
func NewMemoryKVCache(ttl time.Duration) *MemoryKVCache {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &MemoryKVCache{
		data: make(map[string]cacheEntry),
		ttl:  ttl,
	}
}

// Store persists the message slice in memory.
func (m *MemoryKVCache) Store(
	_ context.Context,
	sessionID string,
	messages []types.InternalMessage,
) error {
	raw, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("memory kvcache: marshal: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[sessionID] = cacheEntry{
		data:      raw,
		expiresAt: time.Now().Add(m.ttl),
	}
	return nil
}

// Retrieve loads a session from the in-memory store. Returns nil, nil if not
// found or expired.
func (m *MemoryKVCache) Retrieve(
	_ context.Context,
	sessionID string,
) ([]types.InternalMessage, error) {
	m.mu.RLock()
	entry, ok := m.data[sessionID]
	m.mu.RUnlock()
	if !ok {
		return nil, nil
	}
	if time.Now().After(entry.expiresAt) {
		// Lazy eviction.
		m.mu.Lock()
		delete(m.data, sessionID)
		m.mu.Unlock()
		return nil, nil
	}
	var msgs []types.InternalMessage
	if err := json.Unmarshal(entry.data, &msgs); err != nil {
		return nil, fmt.Errorf("memory kvcache: unmarshal: %w", err)
	}
	return msgs, nil
}

// Invalidate removes a session from the in-memory store.
func (m *MemoryKVCache) Invalidate(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, sessionID)
	return nil
}

// Stats returns the number of live (non-expired) entries and a static memory
// placeholder.
func (m *MemoryKVCache) Stats(_ context.Context) (*CacheStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	var live int64
	for _, e := range m.data {
		if now.Before(e.expiresAt) {
			live++
		}
	}
	return &CacheStats{
		TotalSessions: live,
		MemoryUsed:    "in-memory",
	}, nil
}

// Available always returns true for the in-memory backend.
func (m *MemoryKVCache) Available() bool { return true }

// Close is a no-op for the in-memory backend.
func (m *MemoryKVCache) Close() error { return nil }
