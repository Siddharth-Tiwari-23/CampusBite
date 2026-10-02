package cache

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrCacheMiss is returned when an item is not found in cache.
	ErrCacheMiss = errors.New("cache miss")
)

const (
	// KeyMenuAvailable is the standard Redis key for caching the available menu items catalog.
	KeyMenuAvailable = "menu:available"
)

// CacheService defines standard caching and rate limiting capabilities.
type CacheService interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Ping(ctx context.Context) error
	Allow(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, remaining int, retryAfter time.Duration, err error)
	Close() error
}

// NoOpCache provides a graceful fallback implementation that performs no caching and permits all rate limits.
type NoOpCache struct{}

// NewNoOpCache returns a no-op cache implementation.
func NewNoOpCache() *NoOpCache {
	return &NoOpCache{}
}

func (n *NoOpCache) Get(ctx context.Context, key string) (string, error) {
	return "", ErrCacheMiss
}

func (n *NoOpCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return nil
}

func (n *NoOpCache) Del(ctx context.Context, keys ...string) error {
	return nil
}

func (n *NoOpCache) Ping(ctx context.Context) error {
	return nil
}

func (n *NoOpCache) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Duration, error) {
	// Fail-open: allow all requests
	return true, limit, 0, nil
}

func (n *NoOpCache) Close() error {
	return nil
}
