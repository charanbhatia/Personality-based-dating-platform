// Package queue is a Redis Streams job queue with consumer groups, delayed
// retries and a dead-letter stream.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

const (
	deadLetterSuffix = ":dead"
	fieldEventID     = "event_id"
	fieldType        = "type"
	fieldPayload     = "payload"
	fieldCreatedAt   = "created_at"
)

type Event struct {
	ID        string
	Type      string
	Payload   json.RawMessage
	CreatedAt time.Time
	Attempt   int

	stream   string
	streamID string
}

// Decode unmarshals the event payload into v.
func (e Event) Decode(v any) error { return json.Unmarshal(e.Payload, v) }

type Handler func(ctx context.Context, e Event) error

type Queue struct {
	client *goredis.Client
	log    *slog.Logger
	maxLen int64
}

func New(client *goredis.Client, log *slog.Logger, maxLen int64) *Queue {
	return &Queue{client: client, log: log, maxLen: maxLen}
}

// Publish appends an event to a stream, trimming the stream to roughly maxLen
// so an unconsumed queue cannot grow without bound.
func (q *Queue) Publish(ctx context.Context, stream, eventType string, payload any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	eventID := uuid.NewString()
	err = q.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: stream,
		MaxLen: q.maxLen,
		Approx: true,
		Values: map[string]any{
			fieldEventID:   eventID,
			fieldType:      eventType,
			fieldPayload:   string(body),
			fieldCreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}).Err()
	if err != nil {
		return "", err
	}
	return eventID, nil
}

type ConsumerConfig struct {
	Stream      string
	Group       string
	Consumer    string
	Concurrency int
	MaxAttempts int
	// RetryDelay is how long a failed event stays pending before another
	// consumer may claim it.
	RetryDelay   time.Duration
	BlockTimeout time.Duration
	// DedupeTTL is how long a completed event_id is remembered so redelivery
	// does not run the handler twice.
	DedupeTTL time.Duration
}

func (c *ConsumerConfig) applyDefaults() {
	if c.Concurrency <= 0 {
		c.Concurrency = 4
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 5
	}
	if c.RetryDelay <= 0 {
		c.RetryDelay = 30 * time.Second
	}
	if c.BlockTimeout <= 0 {
		c.BlockTimeout = 5 * time.Second
	}
	if c.DedupeTTL <= 0 {
		c.DedupeTTL = 24 * time.Hour
	}
	if c.Consumer == "" {
		c.Consumer = uuid.NewString()
	}
}

// Consume blocks until ctx is cancelled, dispatching events to handler. New
// events and reclaimed ones are fed to the same pool of workers.
func (q *Queue) Consume(ctx context.Context, cfg ConsumerConfig, handler Handler) error {
	cfg.applyDefaults()

	if err := q.client.XGroupCreateMkStream(ctx, cfg.Stream, cfg.Group, "0").Err(); err != nil &&
		!errors.Is(err, goredis.Nil) && !isBusyGroup(err) {
		return fmt.Errorf("create consumer group %s/%s: %w", cfg.Stream, cfg.Group, err)
	}

	events := make(chan Event)
	var workers sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for e := range events {
				q.process(ctx, cfg, handler, e)
			}
		}()
	}

	var producers sync.WaitGroup
	producers.Add(2)
	go func() {
		defer producers.Done()
		q.readNew(ctx, cfg, events)
	}()
	go func() {
		defer producers.Done()
		q.reclaimStale(ctx, cfg, events)
	}()

	producers.Wait()
	close(events)
	workers.Wait()
	return nil
}

func (q *Queue) readNew(ctx context.Context, cfg ConsumerConfig, out chan<- Event) {
	for ctx.Err() == nil {
		streams, err := q.client.XReadGroup(ctx, &goredis.XReadGroupArgs{
			Group:    cfg.Group,
			Consumer: cfg.Consumer,
			Streams:  []string{cfg.Stream, ">"},
			Count:    int64(cfg.Concurrency),
			Block:    cfg.BlockTimeout,
		}).Result()

		if err != nil {
			if errors.Is(err, goredis.Nil) || ctx.Err() != nil {
				continue
			}
			q.log.Error("queue read failed", "stream", cfg.Stream, "error", err)
			sleep(ctx, cfg.BlockTimeout)
			continue
		}

		for _, s := range streams {
			for _, m := range s.Messages {
				if !emit(ctx, out, q.toEvent(cfg.Stream, m)) {
					return
				}
			}
		}
	}
}

// reclaimStale re-delivers events whose handler failed or whose consumer died.
// The minimum idle time is what makes a retry a delayed retry.
func (q *Queue) reclaimStale(ctx context.Context, cfg ConsumerConfig, out chan<- Event) {
	ticker := time.NewTicker(cfg.RetryDelay)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		messages, _, err := q.client.XAutoClaim(ctx, &goredis.XAutoClaimArgs{
			Stream:   cfg.Stream,
			Group:    cfg.Group,
			Consumer: cfg.Consumer,
			MinIdle:  cfg.RetryDelay,
			Start:    "0",
			Count:    int64(cfg.Concurrency),
		}).Result()
		if err != nil {
			if ctx.Err() == nil {
				q.log.Error("queue reclaim failed", "stream", cfg.Stream, "error", err)
			}
			continue
		}

		for _, m := range messages {
			if !emit(ctx, out, q.toEvent(cfg.Stream, m)) {
				return
			}
		}
	}
}

func (q *Queue) process(ctx context.Context, cfg ConsumerConfig, handler Handler, e Event) {
	dedupeKey := fmt.Sprintf("queue:done:%s:%s", cfg.Stream, e.ID)
	done, err := q.client.Exists(ctx, dedupeKey).Result()
	if err == nil && done > 0 {
		q.ack(ctx, cfg, e)
		return
	}

	attempt, err := q.client.Incr(ctx, attemptsKey(cfg.Stream, e.streamID)).Result()
	if err == nil {
		q.client.Expire(ctx, attemptsKey(cfg.Stream, e.streamID), cfg.DedupeTTL)
	}
	e.Attempt = int(attempt)

	if err := handler(ctx, e); err != nil {
		q.log.Error("queue handler failed",
			"stream", cfg.Stream, "event_id", e.ID, "type", e.Type, "attempt", e.Attempt, "error", err)

		if e.Attempt >= cfg.MaxAttempts {
			q.deadLetter(ctx, cfg, e, err)
			q.ack(ctx, cfg, e)
		}
		// Otherwise leave the event pending so the reclaimer retries it.
		return
	}

	q.client.Set(ctx, dedupeKey, "1", cfg.DedupeTTL)
	q.ack(ctx, cfg, e)
}

func (q *Queue) ack(ctx context.Context, cfg ConsumerConfig, e Event) {
	if err := q.client.XAck(ctx, cfg.Stream, cfg.Group, e.streamID).Err(); err != nil && ctx.Err() == nil {
		q.log.Error("queue ack failed", "stream", cfg.Stream, "event_id", e.ID, "error", err)
	}
	q.client.Del(ctx, attemptsKey(cfg.Stream, e.streamID))
}

func (q *Queue) deadLetter(ctx context.Context, cfg ConsumerConfig, e Event, cause error) {
	err := q.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: cfg.Stream + deadLetterSuffix,
		MaxLen: q.maxLen,
		Approx: true,
		Values: map[string]any{
			fieldEventID:   e.ID,
			fieldType:      e.Type,
			fieldPayload:   string(e.Payload),
			fieldCreatedAt: e.CreatedAt.Format(time.RFC3339Nano),
			"attempts":     e.Attempt,
			"error":        cause.Error(),
		},
	}).Err()
	if err != nil {
		q.log.Error("dead letter write failed", "stream", cfg.Stream, "event_id", e.ID, "error", err)
		return
	}
	q.log.Warn("event dead lettered",
		"stream", cfg.Stream, "event_id", e.ID, "type", e.Type, "attempts", e.Attempt)
}

func (q *Queue) toEvent(stream string, m goredis.XMessage) Event {
	e := Event{stream: stream, streamID: m.ID}
	if v, ok := m.Values[fieldEventID].(string); ok {
		e.ID = v
	}
	if v, ok := m.Values[fieldType].(string); ok {
		e.Type = v
	}
	if v, ok := m.Values[fieldPayload].(string); ok {
		e.Payload = json.RawMessage(v)
	}
	if v, ok := m.Values[fieldCreatedAt].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			e.CreatedAt = t
		}
	}
	return e
}

func attemptsKey(stream, streamID string) string {
	return fmt.Sprintf("queue:attempts:%s:%s", stream, streamID)
}

func emit(ctx context.Context, out chan<- Event, e Event) bool {
	select {
	case out <- e:
		return true
	case <-ctx.Done():
		return false
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func isBusyGroup(err error) bool {
	return err != nil && err.Error() == "BUSYGROUP Consumer Group name already exists"
}
