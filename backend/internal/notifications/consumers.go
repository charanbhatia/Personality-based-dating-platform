package notifications

import (
	"context"
	"errors"
	"log/slog"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/events"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/queue"
)

// NotificationsHandler fans domain events out into the in-app inbox. Unknown
// event types are acknowledged rather than retried, so another domain adding an
// event to this stream cannot stall the consumer.
func NotificationsHandler(svc *Service, log *slog.Logger) queue.Handler {
	return func(ctx context.Context, e queue.Event) error {
		switch e.Type {
		case events.TypeMatchCreated:
			var payload events.MatchCreated
			if err := e.Decode(&payload); err != nil {
				return err
			}
			return skipMissingUser(svc.OnMatchCreated(ctx, e.ID, payload), log, e)

		case events.TypeMessageCreated:
			var payload events.MessageCreated
			if err := e.Decode(&payload); err != nil {
				return err
			}
			return skipMissingUser(svc.OnMessageCreated(ctx, e.ID, payload), log, e)

		default:
			log.Debug("ignoring event", "type", e.Type, "event_id", e.ID)
			return nil
		}
	}
}

func EmailHandler(svc *Service, log *slog.Logger) queue.Handler {
	return func(ctx context.Context, e queue.Event) error {
		switch e.Type {
		case events.TypePasswordResetRequested:
			var payload events.PasswordResetRequested
			if err := e.Decode(&payload); err != nil {
				return err
			}
			return skipMissingUser(svc.OnPasswordResetRequested(ctx, payload), log, e)

		case events.TypeEmailVerificationRequested:
			var payload events.EmailVerificationRequested
			if err := e.Decode(&payload); err != nil {
				return err
			}
			return skipMissingUser(svc.OnEmailVerificationRequested(ctx, payload), log, e)

		default:
			log.Debug("ignoring email event", "type", e.Type, "event_id", e.ID)
			return nil
		}
	}
}

// skipMissingUser treats a deleted user as done rather than retrying an event
// that can never succeed.
func skipMissingUser(err error, log *slog.Logger, e queue.Event) error {
	if errors.Is(err, ErrNotFound) {
		log.Warn("dropping event for a user that no longer exists", "type", e.Type, "event_id", e.ID)
		return nil
	}
	return err
}
