package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateLimitScript is an atomic Redis Lua script that increments a counter, sets TTL on first access,
// and returns whether the request is allowed, the current count, remaining quota, and time-to-live.
var rateLimitScript = redis.NewScript(`
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])

local current = redis.call('INCR', key)
if current == 1 then
    redis.call('EXPIRE', key, window)
end

local ttl = redis.call('TTL', key)
if ttl < 0 then
    redis.call('EXPIRE', key, window)
    ttl = window
end

local remaining = limit - current
if remaining < 0 then
    remaining = 0
end

local allowed = 1
if current > limit then
    allowed = 0
end

return {allowed, current, remaining, ttl}
`)

// RedisCache wraps a go-redis client with safe caching and rate limiting capabilities.
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache creates and initializes a RedisCache from a connection URL.
func NewRedisCache(redisURL string) (*RedisCache, error) {
	if strings.TrimSpace(redisURL) == "" {
		return nil, errors.New("redis url cannot be empty")
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		// Fallback for plain host:port formats
		opts = &redis.Options{
			Addr: redisURL,
		}
	}

	// Optimize connection timeouts
	opts.DialTimeout = 500 * time.Millisecond
	opts.ReadTimeout = 500 * time.Millisecond
	opts.WriteTimeout = 500 * time.Millisecond
	opts.PoolSize = 10

	client := redis.NewClient(opts)

	// Verify connectivity with a fast ping check
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("[Redis] Warning: initial Redis ping failed to %s: %v (application will operate in degraded cache mode)", opts.Addr, err)
	} else {
		log.Printf("[Redis] Successfully connected to Redis at %s", opts.Addr)
	}

	return &RedisCache{client: client}, nil
}

// NewRedisCacheFromClient creates a RedisCache from an existing *redis.Client (useful for testing with miniredis).
func NewRedisCacheFromClient(client *redis.Client) *RedisCache {
	return &RedisCache{client: client}
}

// Get retrieves a string value from Redis.
func (r *RedisCache) Get(ctx context.Context, key string) (string, error) {
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", ErrCacheMiss
		}
		return "", fmt.Errorf("redis get error for key '%s': %w", key, err)
	}
	return val, nil
}

// Set stores a value in Redis with the specified TTL. Value can be string, []byte, or any JSON-serializable object.
func (r *RedisCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	var data interface{}
	switch v := value.(type) {
	case string, []byte, int, int64, float64, bool:
		data = v
	default:
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("failed to marshal cache value for key '%s': %w", key, err)
		}
		data = string(jsonBytes)
	}

	err := r.client.Set(ctx, key, data, ttl).Err()
	if err != nil {
		return fmt.Errorf("redis set error for key '%s': %w", key, err)
	}
	return nil
}

// Del removes one or more keys from Redis.
func (r *RedisCache) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	err := r.client.Del(ctx, keys...).Err()
	if err != nil {
		return fmt.Errorf("redis del error for keys %v: %w", keys, err)
	}
	return nil
}

// Ping checks Redis connectivity.
func (r *RedisCache) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// Allow evaluates atomic fixed-window rate limiting for a given key.
// Returns whether the action is allowed, remaining requests, and retry duration if limit is exceeded.
func (r *RedisCache) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Duration, error) {
	windowSeconds := int(window.Seconds())
	if windowSeconds <= 0 {
		windowSeconds = 1
	}

	res, err := rateLimitScript.Run(ctx, r.client, []string{key}, limit, windowSeconds).Result()
	if err != nil {
		// Log error and return error so caller can choose fail-open policy
		return true, limit, 0, fmt.Errorf("redis rate limit eval error: %w", err)
	}

	slice, ok := res.([]interface{})
	if !ok || len(slice) < 4 {
		return true, limit, 0, errors.New("invalid rate limit script result format")
	}

	allowedInt, _ := slice[0].(int64)
	remainingInt, _ := slice[2].(int64)
	ttlInt, _ := slice[3].(int64)

	allowed := allowedInt == 1
	remaining := int(remainingInt)
	retryAfter := time.Duration(ttlInt) * time.Second

	return allowed, remaining, retryAfter, nil
}

// Close closes the underlying Redis client connection pool.
func (r *RedisCache) Close() error {
	return r.client.Close()
}
