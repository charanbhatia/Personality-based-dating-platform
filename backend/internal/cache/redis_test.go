package cache

import (
	"context"
	"os"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func testRedis(t *testing.T) *Redis {
	t.Helper()

	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set; skipping redis cache test")
	}

	opt, err := goredis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	client := goredis.NewClient(opt)
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	return NewRedis(client)
}

func TestRedisGetSetDelete(t *testing.T) {
	ctx := context.Background()
	c := testRedis(t)
	key := "cache-test:" + t.Name()
	t.Cleanup(func() { _ = c.Delete(ctx, key) })

	if _, found, err := c.Get(ctx, key); err != nil {
		t.Fatalf("Get missing: %v", err)
	} else if found {
		t.Fatal("an unset key reported a hit")
	}

	if err := c.Set(ctx, key, []byte("value"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	value, found, err := c.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found || string(value) != "value" {
		t.Fatalf("Get returned (%q, %v)", value, found)
	}

	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, _ := c.Get(ctx, key); found {
		t.Fatal("the key survived Delete")
	}
}

func TestRedisIgnoresNonPositiveTTL(t *testing.T) {
	ctx := context.Background()
	c := testRedis(t)
	key := "cache-test:" + t.Name()
	t.Cleanup(func() { _ = c.Delete(ctx, key) })

	for _, ttl := range []time.Duration{0, -time.Second} {
		if err := c.Set(ctx, key, []byte("v"), ttl); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if _, found, err := c.Get(ctx, key); err != nil {
			t.Fatalf("Get: %v", err)
		} else if found {
			t.Fatalf("ttl %v was stored; it would never expire correctly", ttl)
		}
	}
}

func TestRedisDeleteWithNoKeys(t *testing.T) {
	c := testRedis(t)
	if err := c.Delete(context.Background()); err != nil {
		t.Fatalf("Delete with no keys: %v", err)
	}
}
