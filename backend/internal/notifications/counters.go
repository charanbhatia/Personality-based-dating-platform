package notifications

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

const counterTTL = time.Hour

// Redis holds unread counts as a cache only; Postgres remains the source of
// truth and the counter is rebuilt from it whenever the key is missing.
var (
	incrIfCached = goredis.NewScript(`
		if redis.call('EXISTS', KEYS[1]) == 1 then
			return redis.call('INCR', KEYS[1])
		end
		return -1`)

	decrIfCached = goredis.NewScript(`
		if redis.call('EXISTS', KEYS[1]) == 1 then
			local v = redis.call('DECR', KEYS[1])
			if v < 0 then
				redis.call('SET', KEYS[1], 0)
				return 0
			end
			return v
		end
		return -1`)
)

type counters struct {
	client *goredis.Client
}

func (c *counters) enabled() bool { return c != nil && c.client != nil }

func key(userID uuid.UUID) string { return fmt.Sprintf("notif:unread:%s", userID) }

// cached returns the counter and whether it was present.
func (c *counters) cached(ctx context.Context, userID uuid.UUID) (int64, bool) {
	if !c.enabled() {
		return 0, false
	}
	n, err := c.client.Get(ctx, key(userID)).Int64()
	if err != nil {
		return 0, false
	}
	return n, true
}

func (c *counters) store(ctx context.Context, userID uuid.UUID, n int64) {
	if !c.enabled() {
		return
	}
	c.client.Set(ctx, key(userID), n, counterTTL)
}

func (c *counters) increment(ctx context.Context, userID uuid.UUID) {
	if !c.enabled() {
		return
	}
	_ = incrIfCached.Run(ctx, c.client, []string{key(userID)}).Err()
}

func (c *counters) decrement(ctx context.Context, userID uuid.UUID) {
	if !c.enabled() {
		return
	}
	_ = decrIfCached.Run(ctx, c.client, []string{key(userID)}).Err()
}

func (c *counters) reset(ctx context.Context, userID uuid.UUID) {
	if !c.enabled() {
		return
	}
	c.client.Set(ctx, key(userID), 0, counterTTL)
}
