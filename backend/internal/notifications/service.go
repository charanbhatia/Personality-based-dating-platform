package notifications

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/cursor"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/email"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/events"
	"github.com/bits-assignment/dating-platform/backend/internal/push"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

type Config struct {
	AppBaseURL string
}

type Service struct {
	store    *Store
	counters *counters
	mailer   email.Sender
	cfg      Config
	log      *slog.Logger
	pusher   push.Pusher
}

func NewService(store *Store, redis *goredis.Client, mailer email.Sender, cfg Config, log *slog.Logger) *Service {
	return &Service{
		store:    store,
		counters: &counters{client: redis},
		mailer:   mailer,
		cfg:      cfg,
		log:      log,
		pusher:   push.Nop{},
	}
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, before *cursor.Keyset, limit int) ([]Notification, *string, error) {
	items, err := s.store.list(ctx, userID, before, limit+1)
	if err != nil {
		return nil, nil, err
	}

	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		encoded := cursor.Encode(cursor.Keyset{Time: last.CreatedAt, ID: last.ID})
		next = &encoded
	}
	return items, next, nil
}

// UnreadCount serves the cached counter and rebuilds it from Postgres on a miss.
func (s *Service) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	if n, ok := s.counters.cached(ctx, userID); ok {
		return n, nil
	}

	n, err := s.store.countUnread(ctx, userID)
	if err != nil {
		return 0, err
	}
	s.counters.store(ctx, userID, n)
	return n, nil
}

func (s *Service) MarkRead(ctx context.Context, userID, id uuid.UUID) error {
	changed, err := s.store.markRead(ctx, userID, id)
	if err != nil {
		return err
	}
	if changed {
		s.counters.decrement(ctx, userID)
	}
	return nil
}

func (s *Service) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	if err := s.store.markAllRead(ctx, userID); err != nil {
		return err
	}
	s.counters.reset(ctx, userID)
	return nil
}

// OnMatchCreated notifies both participants of a new mutual match.
func (s *Service) OnMatchCreated(ctx context.Context, eventID string, e events.MatchCreated) error {
	for _, pair := range [2][2]uuid.UUID{
		{e.UserAID, e.UserBID},
		{e.UserBID, e.UserAID},
	} {
		recipient, other := pair[0], pair[1]

		name, err := s.store.userName(ctx, other)
		if err != nil {
			return err
		}

		created, err := s.store.insert(ctx, newNotification{
			UserID:  recipient,
			Type:    events.TypeMatchCreated,
			Title:   "It's a match",
			Body:    fmt.Sprintf("You and %s liked each other.", name),
			Data:    map[string]any{"match_id": e.MatchID, "user_id": other},
			EventID: eventID,
		})
		if err != nil {
			return err
		}
		if created {
			s.counters.increment(ctx, recipient)
			s.pushToUser(ctx, recipient, "It's a match", fmt.Sprintf("You and %s liked each other.", name))
		}
	}
	return nil
}

// OnMessageCreated notifies the recipient only; the sender already knows.
func (s *Service) OnMessageCreated(ctx context.Context, eventID string, e events.MessageCreated) error {
	name, err := s.store.userName(ctx, e.SenderID)
	if err != nil {
		return err
	}

	created, err := s.store.insert(ctx, newNotification{
		UserID: e.RecipientID,
		Type:   events.TypeMessageCreated,
		Title:  name,
		Body:   e.Preview,
		Data: map[string]any{
			"conversation_id": e.ConversationID,
			"message_id":      e.MessageID,
			"sender_id":       e.SenderID,
		},
		EventID: eventID,
	})
	if err != nil {
		return err
	}
	if created {
		s.counters.increment(ctx, e.RecipientID)
		s.pushToUser(ctx, e.RecipientID, name, e.Preview)
	}
	return nil
}

func (s *Service) OnPasswordResetRequested(ctx context.Context, e events.PasswordResetRequested) error {
	address := e.Email
	if address == "" {
		resolved, err := s.store.userEmail(ctx, e.UserID)
		if err != nil {
			return err
		}
		address = resolved
	}

	return s.mailer.Send(ctx, email.Message{
		To:      address,
		Subject: "Reset your password",
		Body: fmt.Sprintf(
			"Use the link below to choose a new password. It expires at %s.\n\n%s/reset-password?token=%s\n",
			e.ExpiresAt.UTC().Format("15:04 MST on 2 Jan 2006"),
			s.cfg.AppBaseURL,
			e.ResetToken,
		),
	})
}

func (s *Service) OnEmailVerificationRequested(ctx context.Context, e events.EmailVerificationRequested) error {
	address := e.Email
	if address == "" {
		resolved, err := s.store.userEmail(ctx, e.UserID)
		if err != nil {
			return err
		}
		address = resolved
	}

	return s.mailer.Send(ctx, email.Message{
		To:      address,
		Subject: "Confirm your email",
		Body: fmt.Sprintf(
			"Use the link below to confirm your email. It expires at %s.\n\n%s/verify-email?token=%s\n",
			e.ExpiresAt.UTC().Format("15:04 MST on 2 Jan 2006"),
			s.cfg.AppBaseURL,
			e.VerifyToken,
		),
	})
}

func (s *Service) RegisterDevice(ctx context.Context, userID uuid.UUID, token, platform string) error {
	platform = strings.ToLower(strings.TrimSpace(platform))
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token is required")
	}
	switch platform {
	case "web", "android", "ios":
	default:
		return fmt.Errorf("platform must be web, android or ios")
	}
	return s.store.upsertDevice(ctx, userID, token, platform)
}

func (s *Service) UnregisterDevice(ctx context.Context, userID uuid.UUID, token string) error {
	return s.store.deleteDevice(ctx, userID, strings.TrimSpace(token))
}

func (s *Service) pushToUser(ctx context.Context, userID uuid.UUID, title, body string) {
	tokens, err := s.store.listDevices(ctx, userID)
	if err != nil {
		if s.log != nil {
			s.log.Warn("listing device tokens failed", "user_id", userID, "error", err)
		}
		return
	}
	if len(tokens) == 0 {
		if s.log != nil {
			s.log.Info("would push", "user_id", userID, "title", title)
		}
		return
	}
	for _, t := range tokens {
		err := s.pusher.Push(ctx, push.Token{UserID: userID, Token: t.Token, Platform: t.Platform}, push.Note{
			Title: title,
			Body:  body,
		})
		if err != nil && s.log != nil {
			s.log.Warn("push failed", "user_id", userID, "platform", t.Platform, "error", err)
		}
	}
}
