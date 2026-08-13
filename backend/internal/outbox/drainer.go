package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/google/uuid"
)

// Defaults for a Drainer left partially configured.
const (
	DefaultBatchSize    = 100
	DefaultPollInterval = 2 * time.Second
	DefaultMaxAttempts  = 10
	// baseBackoff is the first retry delay; it doubles per attempt up to
	// maxBackoff.
	baseBackoff = 5 * time.Second
	maxBackoff  = 10 * time.Minute
)

// claimSQL leases a batch of pending rows.
//
// SKIP LOCKED lets several drainers (API replica plus Person C's worker) run
// concurrently without double-publishing or blocking each other. Claiming also
// advances available_at, which doubles as the lease: if this process dies mid
// publish, the row becomes eligible again after the backoff rather than needing
// a separate reaper.
const claimSQL = `
UPDATE outbox_events o
SET attempts     = o.attempts + 1,
    available_at = now() + (interval '1 second' * $3)
                          * power(2, least(o.attempts, 6))
FROM (
    SELECT id
    FROM outbox_events
    WHERE published_at IS NULL
      AND available_at <= now()
      AND attempts < $1
    ORDER BY available_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT $2
) AS claimed
WHERE o.id = claimed.id
RETURNING o.id, o.event_type, o.payload, o.created_at, o.attempts`

// Drainer publishes pending outbox rows.
type Drainer struct {
	Pool interface {
		db.Querier
		db.Beginner
	}
	Publisher    Publisher
	BatchSize    int
	PollInterval time.Duration
	MaxAttempts  int

	// kick wakes the loop immediately after a domain write commits, so an event
	// is normally published within milliseconds instead of waiting a full tick.
	kick chan struct{}
}

// NewDrainer builds a Drainer, filling in defaults.
func NewDrainer(pool interface {
	db.Querier
	db.Beginner
}, publisher Publisher, batchSize int, pollInterval time.Duration, maxAttempts int) *Drainer {
	if publisher == nil {
		publisher = SlogPublisher{}
	}
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	return &Drainer{
		Pool:         pool,
		Publisher:    publisher,
		BatchSize:    batchSize,
		PollInterval: pollInterval,
		MaxAttempts:  maxAttempts,
		kick:         make(chan struct{}, 1),
	}
}

// Kick asks the drainer to run a pass now. It never blocks, so a request handler
// can call it on the hot path after committing.
func (d *Drainer) Kick() {
	if d == nil || d.kick == nil {
		return
	}
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

// Run drains until ctx is cancelled. Intended to be started in its own
// goroutine; it returns nil on a clean shutdown.
func (d *Drainer) Run(ctx context.Context) error {
	ticker := time.NewTicker(d.PollInterval)
	defer ticker.Stop()

	slog.Info("outbox drainer started",
		"batch_size", d.BatchSize,
		"poll_interval", d.PollInterval,
		"max_attempts", d.MaxAttempts,
	)

	for {
		// Keep draining while full batches come back, so a backlog clears at
		// transport speed rather than one batch per tick.
		for {
			published, err := d.DrainOnce(ctx)
			if err != nil {
				if ctx.Err() != nil {
					break
				}
				slog.Error("outbox drain failed", "error", err)
				break
			}
			if published < d.BatchSize {
				break
			}
		}

		select {
		case <-ctx.Done():
			slog.Info("outbox drainer stopped")
			return nil
		case <-ticker.C:
		case <-d.kick:
		}
	}
}

// DrainOnce claims and publishes at most BatchSize events, returning how many
// were published successfully.
func (d *Drainer) DrainOnce(ctx context.Context) (int, error) {
	events, err := d.claim(ctx)
	if err != nil {
		return 0, err
	}

	published := 0
	for _, e := range events {
		if ctx.Err() != nil {
			return published, ctx.Err()
		}
		if err := d.Publisher.Publish(ctx, e.Event); err != nil {
			d.recordFailure(ctx, e, err)
			continue
		}
		if err := d.markPublished(ctx, e.Event.ID); err != nil {
			// The event reached the transport but the bookkeeping failed, so it
			// will be redelivered. Consumers are required to be idempotent on
			// event_id for exactly this case.
			slog.Error("outbox mark published failed",
				"event_id", e.Event.ID, "event_type", e.Event.Type, "error", err)
			continue
		}
		published++
	}
	return published, nil
}

type claimedEvent struct {
	Event    Event
	Attempts int
}

func (d *Drainer) claim(ctx context.Context) ([]claimedEvent, error) {
	rows, err := d.Pool.Query(ctx, claimSQL, d.MaxAttempts, d.BatchSize, baseBackoff.Seconds())
	if err != nil {
		return nil, fmt.Errorf("claim outbox batch: %w", err)
	}
	defer rows.Close()

	var out []claimedEvent
	for rows.Next() {
		var e claimedEvent
		if err := rows.Scan(&e.Event.ID, &e.Event.Type, &e.Event.Payload, &e.Event.OccurredAt, &e.Attempts); err != nil {
			return nil, fmt.Errorf("scan outbox row: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read outbox batch: %w", err)
	}
	return out, nil
}

func (d *Drainer) markPublished(ctx context.Context, id uuid.UUID) error {
	_, err := d.Pool.Exec(ctx,
		`UPDATE outbox_events SET published_at = now(), last_error = NULL WHERE id = $1 AND published_at IS NULL`,
		id)
	return err
}

func (d *Drainer) recordFailure(ctx context.Context, e claimedEvent, cause error) {
	exhausted := e.Attempts >= d.MaxAttempts
	logger := slog.With(
		"event_id", e.Event.ID,
		"event_type", e.Event.Type,
		"attempts", e.Attempts,
		"error", cause,
	)
	if exhausted {
		// Left unpublished and no longer claimable. Surfacing it loudly is
		// correct: dropping a match notification silently is worse than an alert.
		logger.Error("outbox event exhausted retries; manual intervention required")
	} else {
		logger.Warn("outbox publish failed; will retry")
	}

	// Persist the reason on a cancellation-free context so a shutdown mid-publish
	// still leaves a diagnosis behind.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := d.Pool.Exec(writeCtx,
		`UPDATE outbox_events SET last_error = $2 WHERE id = $1`,
		e.Event.ID, truncate(cause.Error(), 1000),
	); err != nil && !errors.Is(err, context.Canceled) {
		logger.Warn("outbox record failure metadata failed", "write_error", err)
	}
}

// PendingCount reports the unpublished backlog, for readiness checks and the
// M4 reliability review.
func (d *Drainer) PendingCount(ctx context.Context) (int64, error) {
	var n int64
	err := d.Pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox_events WHERE published_at IS NULL`).Scan(&n)
	return n, err
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
