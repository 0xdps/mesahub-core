// Package cache provides a nil-safe Redis abstraction.
// When REDIS_URL is not set, a NoopClient is returned and all operations are
// silent no-ops, allowing callers to fall back to JWT / DB-direct paths.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// SessionValue holds the data stored per session.
type SessionValue struct {
	Role   string `json:"role"`
	UserID string `json:"user_id,omitempty"`
	Email  string `json:"email,omitempty"`
}

// APIKeyValue holds the data cached per hashed API key.
type APIKeyValue struct {
	UserID string `json:"user_id"`
	KeyID  string `json:"key_id"`
}

// PKCEValue holds PKCE state for the OAuth flow.
type PKCEValue struct {
	Verifier    string `json:"verifier"`
	RedirectURI string `json:"redirect_uri"`
}

// Client is the interface every cache backend implements.
type Client interface {
	SetSession(ctx context.Context, id string, v SessionValue, ttl time.Duration) error
	GetSession(ctx context.Context, id string) (*SessionValue, error)
	DeleteSession(ctx context.Context, id string) error

	SetAPIKey(ctx context.Context, hash string, v APIKeyValue, ttl time.Duration) error
	GetAPIKey(ctx context.Context, hash string) (*APIKeyValue, error)

	SetPKCE(ctx context.Context, state string, v PKCEValue, ttl time.Duration) error
	GetPKCE(ctx context.Context, state string) (*PKCEValue, error)

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
func (n *noopClient) SetPKCE(_ context.Context, _ string, _ PKCEValue, _ time.Duration) error {
	return nil
}
func (n *noopClient) GetPKCE(_ context.Context, _ string) (*PKCEValue, error) { return nil, nil }
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

func (r *redisClient) IncrRateLimit(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	pipe := r.rdb.Pipeline()
	incr := pipe.Incr(ctx, "sh:ratelimit:"+key)
	pipe.Expire(ctx, "sh:ratelimit:"+key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}
