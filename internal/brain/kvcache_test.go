package brain_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// ---------------------------------------------------------------------------
// MemoryKVCache — Store / Retrieve
// ---------------------------------------------------------------------------

func TestMemoryKVCache_StoreRetrieve(t *testing.T) {
	cache := brain.NewMemoryKVCache(time.Hour)
	ctx := context.Background()

	msgs := []types.InternalMessage{
		{Role: types.RoleUser, Content: "Hello"},
		{Role: types.RoleAssistant, Content: "Hi there!"},
	}

	err := cache.Store(ctx, "session-1", msgs)
	require.NoError(t, err)

	got, err := cache.Retrieve(ctx, "session-1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, types.RoleUser, got[0].Role)
	assert.Equal(t, "Hello", got[0].Content)
	assert.Equal(t, types.RoleAssistant, got[1].Role)
	assert.Equal(t, "Hi there!", got[1].Content)
}

// ---------------------------------------------------------------------------
// MemoryKVCache — TTL expiry
// ---------------------------------------------------------------------------

func TestMemoryKVCache_TTLExpiry(t *testing.T) {
	cache := brain.NewMemoryKVCache(50 * time.Millisecond)
	ctx := context.Background()

	msgs := []types.InternalMessage{
		{Role: types.RoleUser, Content: "Ephemeral"},
	}

	err := cache.Store(ctx, "session-ttl", msgs)
	require.NoError(t, err)

	// Immediately available.
	got, err := cache.Retrieve(ctx, "session-ttl")
	require.NoError(t, err)
	require.Len(t, got, 1)

	// Wait for TTL to expire.
	time.Sleep(80 * time.Millisecond)

	got, err = cache.Retrieve(ctx, "session-ttl")
	require.NoError(t, err)
	assert.Nil(t, got, "expected nil after TTL expiry")
}

// ---------------------------------------------------------------------------
// MemoryKVCache — Invalidate
// ---------------------------------------------------------------------------

func TestMemoryKVCache_Invalidate(t *testing.T) {
	cache := brain.NewMemoryKVCache(time.Hour)
	ctx := context.Background()

	msgs := []types.InternalMessage{
		{Role: types.RoleUser, Content: "Will be removed"},
	}

	err := cache.Store(ctx, "session-inv", msgs)
	require.NoError(t, err)

	err = cache.Invalidate(ctx, "session-inv")
	require.NoError(t, err)

	got, err := cache.Retrieve(ctx, "session-inv")
	require.NoError(t, err)
	assert.Nil(t, got, "expected nil after invalidation")
}

// ---------------------------------------------------------------------------
// MemoryKVCache — Stats
// ---------------------------------------------------------------------------

func TestMemoryKVCache_Stats(t *testing.T) {
	cache := brain.NewMemoryKVCache(time.Hour)
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		err := cache.Store(ctx, id, []types.InternalMessage{
			{Role: types.RoleUser, Content: "msg-" + id},
		})
		require.NoError(t, err)
	}

	stats, err := cache.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), stats.TotalSessions)
	assert.Equal(t, "in-memory", stats.MemoryUsed)
}

// ---------------------------------------------------------------------------
// MemoryKVCache — Available always true
// ---------------------------------------------------------------------------

func TestMemoryKVCache_Available(t *testing.T) {
	cache := brain.NewMemoryKVCache(time.Hour)
	assert.True(t, cache.Available())
}

// ---------------------------------------------------------------------------
// MemoryKVCache — Retrieve non-existent returns nil
// ---------------------------------------------------------------------------

func TestMemoryKVCache_RetrieveNonExistent(t *testing.T) {
	cache := brain.NewMemoryKVCache(time.Hour)
	ctx := context.Background()

	got, err := cache.Retrieve(ctx, "does-not-exist")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// ---------------------------------------------------------------------------
// MemoryKVCache — Close is a no-op
// ---------------------------------------------------------------------------

func TestMemoryKVCache_Close(t *testing.T) {
	cache := brain.NewMemoryKVCache(time.Hour)
	assert.NoError(t, cache.Close())
}

// ---------------------------------------------------------------------------
// MemoryKVCache — Overwrite existing session
// ---------------------------------------------------------------------------

func TestMemoryKVCache_Overwrite(t *testing.T) {
	cache := brain.NewMemoryKVCache(time.Hour)
	ctx := context.Background()

	err := cache.Store(ctx, "s1", []types.InternalMessage{
		{Role: types.RoleUser, Content: "first"},
	})
	require.NoError(t, err)

	err = cache.Store(ctx, "s1", []types.InternalMessage{
		{Role: types.RoleUser, Content: "second"},
		{Role: types.RoleAssistant, Content: "reply"},
	})
	require.NoError(t, err)

	got, err := cache.Retrieve(ctx, "s1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "second", got[0].Content)
}

// ---------------------------------------------------------------------------
// MemoryKVCache — Stats excludes expired entries
// ---------------------------------------------------------------------------

func TestMemoryKVCache_StatsExcludesExpired(t *testing.T) {
	cache := brain.NewMemoryKVCache(50 * time.Millisecond)
	ctx := context.Background()

	_ = cache.Store(ctx, "live", []types.InternalMessage{
		{Role: types.RoleUser, Content: "alive"},
	})

	// Wait for the entry to expire.
	time.Sleep(80 * time.Millisecond)

	stats, err := cache.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), stats.TotalSessions)
}

// ---------------------------------------------------------------------------
// MemoryKVCache — Default TTL (zero means 1 hour)
// ---------------------------------------------------------------------------

func TestNewMemoryKVCache_DefaultTTL(t *testing.T) {
	cache := brain.NewMemoryKVCache(0)
	ctx := context.Background()

	// Should accept a store (not panic) with zero/default TTL.
	err := cache.Store(ctx, "default-ttl", []types.InternalMessage{
		{Role: types.RoleUser, Content: "hi"},
	})
	require.NoError(t, err)

	got, err := cache.Retrieve(ctx, "default-ttl")
	require.NoError(t, err)
	require.Len(t, got, 1)
}

// ---------------------------------------------------------------------------
// RedisKVCache — graceful degradation when Redis is unreachable
// ---------------------------------------------------------------------------

func TestRedisKVCache_Unavailable(t *testing.T) {
	// Connect to a port where no Redis is listening.
	cache := brain.NewKVCache(brain.KVCacheConfig{
		RedisAddr: "localhost:59999",
	})
	defer cache.Close()

	assert.False(t, cache.Available())

	ctx := context.Background()

	err := cache.Store(ctx, "s1", []types.InternalMessage{
		{Role: types.RoleUser, Content: "hello"},
	})
	assert.Error(t, err)

	_, err = cache.Retrieve(ctx, "s1")
	assert.Error(t, err)

	err = cache.Invalidate(ctx, "s1")
	assert.Error(t, err)

	_, err = cache.Stats(ctx)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestKVCacher_InterfaceCompliance(t *testing.T) {
	// Compile-time check that both backends implement KVCacher.
	var _ brain.KVCacher = (*brain.RedisKVCache)(nil)
	var _ brain.KVCacher = (*brain.MemoryKVCache)(nil)
}

// ---------------------------------------------------------------------------
// MemoryKVCache — messages with tool calls preserved
// ---------------------------------------------------------------------------

func TestMemoryKVCache_ToolCallsPreserved(t *testing.T) {
	cache := brain.NewMemoryKVCache(time.Hour)
	ctx := context.Background()

	msgs := []types.InternalMessage{
		{Role: types.RoleUser, Content: "Search for weather"},
		{
			Role:    types.RoleAssistant,
			Content: "",
			ToolCalls: []types.InternalToolCall{
				{
					ID:   "call_123",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "get_weather",
						Arguments: `{"location":"London"}`,
					},
				},
			},
		},
		{
			Role:       types.RoleTool,
			Content:    `{"temp": 15}`,
			ToolCallID: "call_123",
		},
	}

	err := cache.Store(ctx, "tool-session", msgs)
	require.NoError(t, err)

	got, err := cache.Retrieve(ctx, "tool-session")
	require.NoError(t, err)
	require.Len(t, got, 3)

	assert.Len(t, got[1].ToolCalls, 1)
	assert.Equal(t, "call_123", got[1].ToolCalls[0].ID)
	assert.Equal(t, "get_weather", got[1].ToolCalls[0].Function.Name)
	assert.Equal(t, "call_123", got[2].ToolCallID)
}
