package ratelimit

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (Result, error)
}

// RedisLimiter is a fixed-window counter: the first request in a window sets the
// TTL, subsequent ones increment until the limit is reached.
type RedisLimiter struct {
	client *goredis.Client
}

func NewRedisLimiter(client *goredis.Client) *RedisLimiter {
	return &RedisLimiter{client: client}
}

func (l *RedisLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (Result, error) {
	pipe := l.client.TxPipeline()
	incr := pipe.Incr(ctx, key)
	ttl := pipe.TTL(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil {
		return Result{}, err
	}

	remaining := ttl.Val()
	if remaining < 0 {
		// Counter has no expiry yet (first request of the window).
		if err := l.client.Expire(ctx, key, window).Err(); err != nil {
			return Result{}, err
		}
		remaining = window
	}

	count := int(incr.Val())
	left := limit - count
	if left < 0 {
		left = 0
	}
	if count <= limit {
		return Result{Allowed: true, Limit: limit, Remaining: left}, nil
	}
	return Result{Allowed: false, Limit: limit, RetryAfter: remaining}, nil
}
