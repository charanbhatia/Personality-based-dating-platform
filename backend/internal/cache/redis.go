package cache

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Redis implements Cache over a Redis instance. A miss or a Redis error is
// never load-bearing: callers fall back to Postgres.
type Redis struct {
	client *goredis.Client
}

// NewRedis wraps an existing client. The caller owns the connection lifetime.
func NewRedis(client *goredis.Client) *Redis {
	return &Redis{client: client}
}

func (r *Redis) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := r.client.Get(ctx, key).Bytes()
	if err == goredis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

func (r *Redis) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *Redis) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return r.client.Del(ctx, keys...).Err()
}
