// Package outbox implements the transactional outbox that carries the B→C
// domain events listed in the shared roadmap §7.
//
// Domain writes call Enqueue with the same transaction that performs the state
// change, so an event can never be lost after a commit nor published for a
// rolled-back write. A Drainer then publishes rows through a Publisher, which
// Person C implements over Redis Streams; until that exists the default
// publisher logs, and rows stay durable and replayable either way.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/google/uuid"
)

// Event types owned by Person B. Consumers must be idempotent on event_id.
const (
	EventMatchCreated          = "match.created"
	EventUserBlocked           = "user.blocked"
	EventPasswordResetRequired = "auth.password_reset_requested"
)

// Event is one published message. The wire envelope is flat — event_id,
// event_type and occurred_at sit alongside the payload fields — matching the
// contract table in roadmap §7.
type Event struct {
	ID         uuid.UUID
	Type       string
	OccurredAt time.Time
	Payload    json.RawMessage
}

// Envelope renders the event as the JSON published to the queue. Payload keys
// are merged at the top level; the reserved envelope keys always win so a
// payload cannot spoof them.
func (e Event) Envelope() ([]byte, error) {
	fields := map[string]any{}
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &fields); err != nil {
			return nil, fmt.Errorf("decode payload for event %s: %w", e.ID, err)
		}
	}
	fields["event_id"] = e.ID
	fields["event_type"] = e.Type
	fields["occurred_at"] = e.OccurredAt.UTC().Format(time.RFC3339Nano)
	return json.Marshal(fields)
}

// Publisher hands an event to the message transport. Person C supplies the
// Redis Streams implementation; a non-nil error causes the row to be retried
// with backoff.
type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

// PublisherFunc adapts a function to Publisher.
type PublisherFunc func(ctx context.Context, event Event) error

func (f PublisherFunc) Publish(ctx context.Context, event Event) error { return f(ctx, event) }

// SlogPublisher is the default until Person C wires the queue. It records the
// envelope so the B→C handoff is observable end to end in local development.
type SlogPublisher struct{}

func (SlogPublisher) Publish(ctx context.Context, event Event) error {
	envelope, err := event.Envelope()
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "domain event published",
		"event_id", event.ID,
		"event_type", event.Type,
		"envelope", string(envelope),
	)
	return nil
}

// Enqueue records an event inside the caller's transaction. Pass the pgx.Tx that
// performs the state change; passing the pool instead silently gives up the
// atomicity this package exists to provide.
func Enqueue(ctx context.Context, q db.Querier, eventType string, payload any) (uuid.UUID, error) {
	if eventType == "" {
		return uuid.Nil, fmt.Errorf("outbox: event type is required")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: encode %s payload: %w", eventType, err)
	}
	var id uuid.UUID
	err = q.QueryRow(ctx,
		`INSERT INTO outbox_events (event_type, payload) VALUES ($1, $2) RETURNING id`,
		eventType, encoded,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: enqueue %s: %w", eventType, err)
	}
	return id, nil
}
