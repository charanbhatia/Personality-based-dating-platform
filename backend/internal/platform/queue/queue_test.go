package queue

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func testQueue(t *testing.T) (*Queue, *goredis.Client) {
	t.Helper()

	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set; skipping queue integration test")
	}

	opt, err := goredis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	client := goredis.NewClient(opt)

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}
	t.Cleanup(func() {
		_ = client.FlushDB(context.Background())
		_ = client.Close()
	})

	return New(client, slog.New(slog.NewTextHandler(io.Discard, nil)), 1000), client
}

type payload struct {
	Value string `json:"value"`
}

// consume runs a consumer until stop fires, so tests do not leak goroutines.
func consume(t *testing.T, q *Queue, cfg ConsumerConfig, h Handler) (stop func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := q.Consume(ctx, cfg, h); err != nil {
			t.Errorf("consume: %v", err)
		}
	}()

	stopped := false
	return func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		<-done
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPublishedEventsReachTheHandler(t *testing.T) {
	q, _ := testQueue(t)

	var mu sync.Mutex
	var got []string

	stop := consume(t, q, ConsumerConfig{
		Stream:       "queue:test",
		Group:        "g1",
		Consumer:     "c1",
		Concurrency:  2,
		BlockTimeout: 100 * time.Millisecond,
	}, func(_ context.Context, e Event) error {
		var p payload
		if err := e.Decode(&p); err != nil {
			return err
		}
		mu.Lock()
		got = append(got, p.Value)
		mu.Unlock()
		return nil
	})
	defer stop()

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := q.Publish(ctx, "queue:test", "test.event", payload{Value: fmt.Sprintf("v%d", i)}); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	waitFor(t, "all events handled", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 5
	})
}

func TestFailedEventsAreRetriedThenDeadLettered(t *testing.T) {
	q, client := testQueue(t)

	var mu sync.Mutex
	attempts := 0

	stop := consume(t, q, ConsumerConfig{
		Stream:       "queue:retry",
		Group:        "g1",
		Consumer:     "c1",
		Concurrency:  1,
		MaxAttempts:  3,
		RetryDelay:   150 * time.Millisecond,
		BlockTimeout: 100 * time.Millisecond,
	}, func(_ context.Context, _ Event) error {
		mu.Lock()
		attempts++
		mu.Unlock()
		return errors.New("handler always fails")
	})
	defer stop()

	ctx := context.Background()
	if _, err := q.Publish(ctx, "queue:retry", "test.event", payload{Value: "boom"}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	waitFor(t, "the event to be dead lettered", func() bool {
		n, err := client.XLen(ctx, "queue:retry:dead").Result()
		return err == nil && n == 1
	})

	mu.Lock()
	total := attempts
	mu.Unlock()
	if total != 3 {
		t.Errorf("handler ran %d times, want 3 (MaxAttempts)", total)
	}

	// A dead lettered event must be acknowledged so it stops being redelivered.
	pending, err := client.XPending(ctx, "queue:retry", "g1").Result()
	if err != nil {
		t.Fatalf("xpending: %v", err)
	}
	if pending.Count != 0 {
		t.Errorf("%d events still pending, want 0", pending.Count)
	}
}

func TestSucceedingRetryIsNotDeadLettered(t *testing.T) {
	q, client := testQueue(t)

	var mu sync.Mutex
	attempts := 0

	stop := consume(t, q, ConsumerConfig{
		Stream:       "queue:flaky",
		Group:        "g1",
		Consumer:     "c1",
		Concurrency:  1,
		MaxAttempts:  5,
		RetryDelay:   150 * time.Millisecond,
		BlockTimeout: 100 * time.Millisecond,
	}, func(_ context.Context, _ Event) error {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n < 2 {
			return errors.New("transient failure")
		}
		return nil
	})
	defer stop()

	ctx := context.Background()
	if _, err := q.Publish(ctx, "queue:flaky", "test.event", payload{Value: "retry me"}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	waitFor(t, "the retry to succeed", func() bool {
		pending, err := client.XPending(ctx, "queue:flaky", "g1").Result()
		return err == nil && pending.Count == 0
	})

	n, err := client.XLen(ctx, "queue:flaky:dead").Result()
	if err != nil {
		t.Fatalf("dead letter length: %v", err)
	}
	if n != 0 {
		t.Errorf("dead letter has %d events, want 0", n)
	}
}

func TestRedeliveryDoesNotRunTheHandlerTwice(t *testing.T) {
	q, client := testQueue(t)
	ctx := context.Background()

	var mu sync.Mutex
	runs := 0
	handler := func(_ context.Context, _ Event) error {
		mu.Lock()
		runs++
		mu.Unlock()
		return nil
	}

	cfg := ConsumerConfig{
		Stream:       "queue:dedupe",
		Group:        "g1",
		Consumer:     "c1",
		Concurrency:  1,
		BlockTimeout: 100 * time.Millisecond,
	}

	stop := consume(t, q, cfg, handler)
	if _, err := q.Publish(ctx, "queue:dedupe", "test.event", payload{Value: "once"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	waitFor(t, "the first delivery", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return runs == 1
	})
	stop()

	// Replay the same event_id as if the stream had redelivered it.
	entries, err := client.XRange(ctx, "queue:dedupe", "-", "+").Result()
	if err != nil || len(entries) != 1 {
		t.Fatalf("read stream: %v (%d entries)", err, len(entries))
	}
	if err := client.XAdd(ctx, &goredis.XAddArgs{
		Stream: "queue:dedupe",
		Values: entries[0].Values,
	}).Err(); err != nil {
		t.Fatalf("replay: %v", err)
	}

	stop2 := consume(t, q, cfg, handler)
	defer stop2()

	waitFor(t, "the replay to be acknowledged", func() bool {
		pending, err := client.XPending(ctx, "queue:dedupe", "g1").Result()
		return err == nil && pending.Count == 0
	})

	mu.Lock()
	defer mu.Unlock()
	if runs != 1 {
		t.Errorf("handler ran %d times, want 1: a redelivered event_id must be skipped", runs)
	}
}
