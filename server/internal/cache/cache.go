// Package cache provides pluggable cache backends for the template server.
//
// CACHE_MODE supports these canonical values:
//
//   - "off"       — NoopClient; all operations silently no-op. Sessions fall
//     back to signed JWTs. API keys hit SQLite on every request.
//   - "redis"     — Redis-backed (requires REDIS_URL). Opaque session tokens
//     stored server-side. API keys cached with a 5-minute TTL.
//   - "local"     — Process-local map with TTL. Same behaviour as Redis but
//     data is lost on restart. No external dependency required.
//
// Legacy aliases remain accepted for backward compatibility:
//   - none -> off
//   - in-memory -> local
//   - both (unchanged; layered local L1 + Redis L2)
//
// Layered mode:
//   - "both"      — In-memory L1 + Redis L2. Reads hit memory first, fall
//     through to Redis and repopulate L1. Writes go to both.
//     Ideal for multi-instance deployments that want sub-ms
//     local reads without sacrificing cross-instance consistency.
//
// The factory function New() picks the right implementation from the mode
// string and optional REDIS_URL.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache mode constants — canonical CACHE_MODE values.
const (
	ModeOff   = "off"
	ModeRedis = "redis"
	ModeLocal = "local"

	// Backward-compatible legacy aliases.
	ModeNone   = "none"
	ModeMemory = "in-memory"

	ModeBoth = "both"
)

// l1APIKeyTTL is the L1 repopulation TTL used when the layered client
// populates the in-memory cache after a Redis hit for API keys.
// Intentionally short: revocations are reflected within this window.
const l1APIKeyTTL = 5 * time.Minute

// SessionValue holds the data stored per session.
type SessionValue struct {
	Role   string `json:"role"`
	UserID string `json:"user_id,omitempty"`
	Email  string `json:"email,omitempty"`
}

// APIKeyValue holds the data cached per hashed API key.
// Scopes is a JSON-encoded array of permission strings using the format
// "<type>:<target>:<level>" where level is "r" (read-only) or "w" (read+write).
// Examples: ["all:w"], ["db:*:r"], ["db:D-abc-mydb:w"], ["bucket:B-abc-photos:r"]
type APIKeyValue struct {
	UserID string   `json:"user_id"`
	KeyID  string   `json:"key_id"`
	Scopes []string `json:"scopes"`
}

// PKCEValue holds PKCE state for the OAuth flow.
type PKCEValue struct {
	Verifier    string `json:"verifier"`
	RedirectURI string `json:"redirect_uri"`
	ReturnTo    string `json:"return_to,omitempty"`
}

// Client is the interface every cache backend implements.
type Client interface {
	SetSession(ctx context.Context, id string, v SessionValue, ttl time.Duration) error
	GetSession(ctx context.Context, id string) (*SessionValue, error)
	DeleteSession(ctx context.Context, id string) error

	SetAPIKey(ctx context.Context, hash string, v APIKeyValue, ttl time.Duration) error
	GetAPIKey(ctx context.Context, hash string) (*APIKeyValue, error)
	// DeleteAPIKey immediately evicts a cached API key. Called by template's
	// internal endpoint when control notifies it of a revocation.
	DeleteAPIKey(ctx context.Context, hash string) error

	SetPKCE(ctx context.Context, state string, v PKCEValue, ttl time.Duration) error
	GetPKCE(ctx context.Context, state string) (*PKCEValue, error)
	// DeletePKCE removes a PKCE state entry after the OAuth callback consumes it,
	// preventing replay attacks within the TTL window.
	DeletePKCE(ctx context.Context, state string) error

	IncrRateLimit(ctx context.Context, key string, ttl time.Duration) (int64, error)

	// Ping checks connectivity; always returns nil for NoopClient.
	Ping(ctx context.Context) error
	// Available reports whether a real Redis connection is backing this client.
	Available() bool
}

// ── NoopClient ───────────────────────────────────────────────────────────────

type noopClient struct{}

// NewNoop returns a NoopClient. All writes are discarded; reads return nil.
func NewNoop() Client { return &noopClient{} }

func (n *noopClient) SetSession(_ context.Context, _ string, _ SessionValue, _ time.Duration) error {
	return nil
}
func (n *noopClient) GetSession(_ context.Context, _ string) (*SessionValue, error) { return nil, nil }
func (n *noopClient) DeleteSession(_ context.Context, _ string) error               { return nil }
func (n *noopClient) SetAPIKey(_ context.Context, _ string, _ APIKeyValue, _ time.Duration) error {
	return nil
}
func (n *noopClient) GetAPIKey(_ context.Context, _ string) (*APIKeyValue, error) { return nil, nil }
func (n *noopClient) DeleteAPIKey(_ context.Context, _ string) error              { return nil }
func (n *noopClient) SetPKCE(_ context.Context, _ string, _ PKCEValue, _ time.Duration) error {
	return nil
}
func (n *noopClient) GetPKCE(_ context.Context, _ string) (*PKCEValue, error) { return nil, nil }
func (n *noopClient) DeletePKCE(_ context.Context, _ string) error            { return nil }
func (n *noopClient) IncrRateLimit(_ context.Context, _ string, _ time.Duration) (int64, error) {
	return 0, nil
}
func (n *noopClient) Ping(_ context.Context) error { return nil }
func (n *noopClient) Available() bool              { return false }

// ── RedisClient ──────────────────────────────────────────────────────────────

type redisClient struct {
	rdb *redis.Client
}

// NewRedis connects to Redis and returns a Client. Returns an error if the
// initial PING fails (fail-fast so misconfigured deployments are obvious).
func NewRedis(redisURL string) (Client, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("cache: invalid REDIS_URL: %w", err)
	}
	rdb := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("cache: redis ping failed: %w", err)
	}
	return &redisClient{rdb: rdb}, nil
}

func (r *redisClient) Available() bool { return true }

func (r *redisClient) Ping(ctx context.Context) error {
	return r.rdb.Ping(ctx).Err()
}

func (r *redisClient) SetSession(ctx context.Context, id string, v SessionValue, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return r.rdb.Set(ctx, "sh:session:"+id, b, ttl).Err()
}

func (r *redisClient) GetSession(ctx context.Context, id string) (*SessionValue, error) {
	b, err := r.rdb.Get(ctx, "sh:session:"+id).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var v SessionValue
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *redisClient) DeleteSession(ctx context.Context, id string) error {
	return r.rdb.Del(ctx, "sh:session:"+id).Err()
}

func (r *redisClient) SetAPIKey(ctx context.Context, hash string, v APIKeyValue, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return r.rdb.Set(ctx, "sh:apikey:"+hash, b, ttl).Err()
}

func (r *redisClient) GetAPIKey(ctx context.Context, hash string) (*APIKeyValue, error) {
	b, err := r.rdb.Get(ctx, "sh:apikey:"+hash).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var v APIKeyValue
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *redisClient) SetPKCE(ctx context.Context, state string, v PKCEValue, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return r.rdb.Set(ctx, "sh:pkce:"+state, b, ttl).Err()
}

func (r *redisClient) GetPKCE(ctx context.Context, state string) (*PKCEValue, error) {
	b, err := r.rdb.Get(ctx, "sh:pkce:"+state).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var v PKCEValue
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *redisClient) DeleteAPIKey(ctx context.Context, hash string) error {
	return r.rdb.Del(ctx, "sh:apikey:"+hash).Err()
}

func (r *redisClient) DeletePKCE(ctx context.Context, state string) error {
	return r.rdb.Del(ctx, "sh:pkce:"+state).Err()
}

func (r *redisClient) IncrRateLimit(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	pipe := r.rdb.Pipeline()
	incr := pipe.Incr(ctx, "sh:ratelimit:"+key)
	pipe.Expire(ctx, "sh:ratelimit:"+key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

// ── InMemoryClient ───────────────────────────────────────────────────────────

type memEntry struct {
	data      []byte
	expiresAt time.Time
}

// memClient is a process-local cache backed by a sync.RWMutex-guarded map.
// Entries expire lazily on read (no background goroutine required).
type memClient struct {
	mu      sync.RWMutex
	entries map[string]memEntry
}

func newMemory() *memClient {
	return &memClient{entries: make(map[string]memEntry)}
}

// NewMemory returns a standalone in-memory Client.
func NewMemory() Client { return newMemory() }

func (m *memClient) Available() bool              { return true }
func (m *memClient) Ping(_ context.Context) error { return nil }

func (m *memClient) mset(key string, v any, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.entries[key] = memEntry{data: b, expiresAt: time.Now().Add(ttl)}
	m.mu.Unlock()
	return nil
}

func (m *memClient) mget(key string, dst any) (bool, error) {
	m.mu.RLock()
	e, ok := m.entries[key]
	m.mu.RUnlock()
	if !ok {
		return false, nil
	}
	if time.Now().After(e.expiresAt) {
		m.mu.Lock()
		delete(m.entries, key) // lazy evict
		m.mu.Unlock()
		return false, nil
	}
	return true, json.Unmarshal(e.data, dst)
}

func (m *memClient) mdel(key string) {
	m.mu.Lock()
	delete(m.entries, key)
	m.mu.Unlock()
}

func (m *memClient) SetSession(_ context.Context, id string, v SessionValue, ttl time.Duration) error {
	return m.mset("sh:session:"+id, v, ttl)
}
func (m *memClient) GetSession(_ context.Context, id string) (*SessionValue, error) {
	var v SessionValue
	ok, err := m.mget("sh:session:"+id, &v)
	if !ok || err != nil {
		return nil, err
	}
	return &v, nil
}
func (m *memClient) DeleteSession(_ context.Context, id string) error {
	m.mdel("sh:session:" + id)
	return nil
}

func (m *memClient) SetAPIKey(_ context.Context, hash string, v APIKeyValue, ttl time.Duration) error {
	return m.mset("sh:apikey:"+hash, v, ttl)
}
func (m *memClient) GetAPIKey(_ context.Context, hash string) (*APIKeyValue, error) {
	var v APIKeyValue
	ok, err := m.mget("sh:apikey:"+hash, &v)
	if !ok || err != nil {
		return nil, err
	}
	return &v, nil
}
func (m *memClient) DeleteAPIKey(_ context.Context, hash string) error {
	m.mdel("sh:apikey:" + hash)
	return nil
}

func (m *memClient) SetPKCE(_ context.Context, state string, v PKCEValue, ttl time.Duration) error {
	return m.mset("sh:pkce:"+state, v, ttl)
}
func (m *memClient) GetPKCE(_ context.Context, state string) (*PKCEValue, error) {
	var v PKCEValue
	ok, err := m.mget("sh:pkce:"+state, &v)
	if !ok || err != nil {
		return nil, err
	}
	return &v, nil
}
func (m *memClient) DeletePKCE(_ context.Context, state string) error {
	m.mdel("sh:pkce:" + state)
	return nil
}

func (m *memClient) IncrRateLimit(_ context.Context, key string, ttl time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := "sh:ratelimit:" + key
	now := time.Now()
	e, ok := m.entries[k]
	if !ok || now.After(e.expiresAt) {
		b, _ := json.Marshal(int64(1))
		m.entries[k] = memEntry{data: b, expiresAt: now.Add(ttl)}
		return 1, nil
	}
	var count int64
	_ = json.Unmarshal(e.data, &count)
	count++
	b, _ := json.Marshal(count)
	m.entries[k] = memEntry{data: b, expiresAt: e.expiresAt} // preserve original window
	return count, nil
}

// ── LayeredClient (in-memory L1 + Redis L2) ──────────────────────────────────

// layeredClient reads from L1 (memory) first and falls through to L2 (Redis)
// on a miss, populating L1 for subsequent requests. Writes and deletes go to
// both layers. Rate limiting is always delegated to Redis for cross-instance
// accuracy; it falls back to L1 only when Redis is unavailable (should not
// happen in a correctly configured deployment).
type layeredClient struct {
	l1 *memClient
	l2 *redisClient
}

func (l *layeredClient) Available() bool                { return true }
func (l *layeredClient) Ping(ctx context.Context) error { return l.l2.Ping(ctx) }

func (l *layeredClient) SetSession(ctx context.Context, id string, v SessionValue, ttl time.Duration) error {
	_ = l.l1.SetSession(ctx, id, v, ttl)
	return l.l2.SetSession(ctx, id, v, ttl)
}
func (l *layeredClient) GetSession(ctx context.Context, id string) (*SessionValue, error) {
	// Sessions intentionally bypass L1: a logout on any instance deletes from
	// Redis immediately, and a stale L1 entry on another instance would continue
	// accepting requests for up to l1SessionTTL after logout. Admin sessions
	// are low-volume so the Redis round-trip cost is acceptable.
	return l.l2.GetSession(ctx, id)
}
func (l *layeredClient) DeleteSession(ctx context.Context, id string) error {
	_ = l.l1.DeleteSession(ctx, id) // evict if present (e.g. from a prior SetSession)
	return l.l2.DeleteSession(ctx, id)
}

func (l *layeredClient) SetAPIKey(ctx context.Context, hash string, v APIKeyValue, ttl time.Duration) error {
	_ = l.l1.SetAPIKey(ctx, hash, v, ttl)
	return l.l2.SetAPIKey(ctx, hash, v, ttl)
}
func (l *layeredClient) GetAPIKey(ctx context.Context, hash string) (*APIKeyValue, error) {
	if kv, err := l.l1.GetAPIKey(ctx, hash); err == nil && kv != nil {
		return kv, nil
	}
	kv, err := l.l2.GetAPIKey(ctx, hash)
	if err == nil && kv != nil {
		_ = l.l1.SetAPIKey(ctx, hash, *kv, l1APIKeyTTL) // repopulate L1
	}
	return kv, err
}
func (l *layeredClient) DeleteAPIKey(ctx context.Context, hash string) error {
	_ = l.l1.DeleteAPIKey(ctx, hash)
	return l.l2.DeleteAPIKey(ctx, hash)
}

func (l *layeredClient) SetPKCE(ctx context.Context, state string, v PKCEValue, ttl time.Duration) error {
	// PKCE state is single-use: store only in Redis so DeletePKCE on any
	// instance invalidates it globally, preventing cross-instance replay.
	return l.l2.SetPKCE(ctx, state, v, ttl)
}
func (l *layeredClient) GetPKCE(ctx context.Context, state string) (*PKCEValue, error) {
	return l.l2.GetPKCE(ctx, state)
}
func (l *layeredClient) DeletePKCE(ctx context.Context, state string) error {
	_ = l.l1.DeletePKCE(ctx, state) // evict if present
	return l.l2.DeletePKCE(ctx, state)
}

func (l *layeredClient) IncrRateLimit(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	return l.l2.IncrRateLimit(ctx, key, ttl)
}

// ── Factory ───────────────────────────────────────────────────────────────────

// New creates a Client from the mode string and optional redisURL.
//
// If mode is empty, it is derived automatically: "redis" when redisURL is
// non-empty, "off" otherwise.
func New(mode, redisURL string) (Client, error) {
	mode = normalizeMode(mode)

	if mode == "" {
		if redisURL != "" {
			mode = ModeRedis
		} else {
			mode = ModeOff
		}
	}

	switch mode {
	case ModeOff:
		return &noopClient{}, nil

	case ModeRedis:
		if redisURL == "" {
			return nil, fmt.Errorf("cache: REDIS_URL is required for mode %q", ModeRedis)
		}
		return NewRedis(redisURL)

	case ModeLocal:
		return newMemory(), nil

	case ModeBoth:
		if redisURL == "" {
			return nil, fmt.Errorf("cache: REDIS_URL is required for mode %q", ModeBoth)
		}
		rdb, err := NewRedis(redisURL)
		if err != nil {
			return nil, err
		}
		return &layeredClient{l1: newMemory(), l2: rdb.(*redisClient)}, nil

	default:
		return nil, fmt.Errorf("cache: unknown mode %q — valid values: off, local, redis (legacy: none, in-memory, both)", mode)
	}
}

func normalizeMode(mode string) string {
	switch mode {
	case ModeNone:
		return ModeOff
	case ModeMemory:
		return ModeLocal
	default:
		return mode
	}
}
