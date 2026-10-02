package cache_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"campusbite/internal/cache"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupTestRedis(t *testing.T) (*cache.RedisCache, *miniredis.Miniredis) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})

	redisCache := cache.NewRedisCacheFromClient(client)
	return redisCache, s
}

func TestRedisCache_GetSetDel(t *testing.T) {
	redisCache, s := setupTestRedis(t)
	defer s.Close()
	defer redisCache.Close()

	ctx := context.Background()

	// 1. Get on non-existent key returns ErrCacheMiss
	_, err := redisCache.Get(ctx, "test:key")
	if err != cache.ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss, got %v", err)
	}

	// 2. Set key with TTL
	err = redisCache.Set(ctx, "test:key", "sample_value", 5*time.Second)
	if err != nil {
		t.Fatalf("failed to set key: %v", err)
	}

	// 3. Get existing key
	val, err := redisCache.Get(ctx, "test:key")
	if err != nil {
		t.Fatalf("failed to get key: %v", err)
	}
	if val != "sample_value" {
		t.Errorf("expected 'sample_value', got '%s'", val)
	}

	// 4. Del key
	err = redisCache.Del(ctx, "test:key")
	if err != nil {
		t.Fatalf("failed to delete key: %v", err)
	}

	// 5. Get after Del returns ErrCacheMiss
	_, err = redisCache.Get(ctx, "test:key")
	if err != cache.ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss after delete, got %v", err)
	}
}

func TestRedisCache_Expiration(t *testing.T) {
	redisCache, s := setupTestRedis(t)
	defer s.Close()
	defer redisCache.Close()

	ctx := context.Background()

	err := redisCache.Set(ctx, "test:expiring", "value", 2*time.Second)
	if err != nil {
		t.Fatalf("failed to set expiring key: %v", err)
	}

	// Fast forward miniredis time by 3 seconds
	s.FastForward(3 * time.Second)

	_, err = redisCache.Get(ctx, "test:expiring")
	if err != cache.ErrCacheMiss {
		t.Errorf("expected key to have expired, got err: %v", err)
	}
}

func TestRedisCache_RateLimit_AtomicAndWindow(t *testing.T) {
	redisCache, s := setupTestRedis(t)
	defer s.Close()
	defer redisCache.Close()

	ctx := context.Background()
	key := "rate:test:user123:checkout"
	limit := 3
	window := 10 * time.Second

	// 1st request -> allowed
	allowed, remaining, _, err := redisCache.Allow(ctx, key, limit, window)
	if err != nil || !allowed || remaining != 2 {
		t.Errorf("req 1: expected allowed=true, remaining=2, got allowed=%v, remaining=%d, err=%v", allowed, remaining, err)
	}

	// 2nd request -> allowed
	allowed, remaining, _, err = redisCache.Allow(ctx, key, limit, window)
	if err != nil || !allowed || remaining != 1 {
		t.Errorf("req 2: expected allowed=true, remaining=1, got allowed=%v, remaining=%d, err=%v", allowed, remaining, err)
	}

	// 3rd request -> allowed (limit reached)
	allowed, remaining, _, err = redisCache.Allow(ctx, key, limit, window)
	if err != nil || !allowed || remaining != 0 {
		t.Errorf("req 3: expected allowed=true, remaining=0, got allowed=%v, remaining=%d, err=%v", allowed, remaining, err)
	}

	// 4th request -> blocked (429)
	allowed, remaining, retryAfter, err := redisCache.Allow(ctx, key, limit, window)
	if err != nil || allowed || remaining != 0 || retryAfter <= 0 {
		t.Errorf("req 4: expected allowed=false, remaining=0, retryAfter>0, got allowed=%v, remaining=%d, retryAfter=%v, err=%v", allowed, remaining, retryAfter, err)
	}

	// Fast forward past window
	s.FastForward(11 * time.Second)

	// 5th request -> allowed again
	allowed, remaining, _, err = redisCache.Allow(ctx, key, limit, window)
	if err != nil || !allowed || remaining != 2 {
		t.Errorf("req 5 after window: expected allowed=true, remaining=2, got allowed=%v, remaining=%d, err=%v", allowed, remaining, err)
	}
}

func TestRedisCache_RateLimit_ConcurrentBypassProtection(t *testing.T) {
	redisCache, s := setupTestRedis(t)
	defer s.Close()
	defer redisCache.Close()

	ctx := context.Background()
	key := "rate:test:concurrent:action"
	limit := 10
	window := 60 * time.Second
	totalRequests := 50

	var wg sync.WaitGroup
	var mu sync.Mutex
	allowedCount := 0
	blockedCount := 0

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed, _, _, err := redisCache.Allow(ctx, key, limit, window)
			if err != nil {
				t.Errorf("unexpected allow error: %v", err)
				return
			}
			mu.Lock()
			if allowed {
				allowedCount++
			} else {
				blockedCount++
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	if allowedCount != limit {
		t.Errorf("expected exactly %d allowed requests under concurrent load, got %d", limit, allowedCount)
	}
	if blockedCount != totalRequests-limit {
		t.Errorf("expected %d blocked requests, got %d", totalRequests-limit, blockedCount)
	}
}
