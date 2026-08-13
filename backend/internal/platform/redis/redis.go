package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type Client = goredis.Client

// New parses a redis:// URL and verifies connectivity before returning.
func New(ctx context.Context, url string) (*Client, error) {
	opt, err := goredis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	c := goredis.NewClient(opt)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.Ping(pingCtx).Err(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}
