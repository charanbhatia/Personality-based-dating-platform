package messaging

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/cursor"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/events"
	"github.com/google/uuid"
)

var (
	ErrForbidden      = errors.New("not a participant in this conversation")
	ErrBlocked        = errors.New("conversation unavailable between these users")
	ErrEmptyContent   = errors.New("message content is required")
	ErrContentTooLong = errors.New("message content is too long")
	ErrGateRequired   = errors.New("a match_id is required to open a conversation")
)

type Config struct {
	MaxMessageLength int
	// MatchGateEnabled requires a mutual match before a conversation can be
	// opened. Disabling it is a development escape hatch for working before
	// Person B's matches table lands.
	MatchGateEnabled bool
}

// Publisher emits domain events. A nil publisher disables event emission, which
// keeps the API usable when Redis is not configured.
type Publisher interface {
	Publish(ctx context.Context, stream, eventType string, payload any) (string, error)
}

type Service struct {
	store *Store
	gate  *MatchGate
	cfg   Config
	pub   Publisher
	log   *slog.Logger
}

func NewService(store *Store, gate *MatchGate, cfg Config, pub Publisher, log *slog.Logger) *Service {
	return &Service{store: store, gate: gate, cfg: cfg, pub: pub, log: log}
}

func (s *Service) MatchGateEnabled() bool { return s.cfg.MatchGateEnabled }

// OpenByMatch creates or returns the conversation authorised by a mutual match.
func (s *Service) OpenByMatch(ctx context.Context, userID, matchID uuid.UUID) (Conversation, error) {
	a, b, err := s.gate.Participants(ctx, matchID)
	if err != nil {
		return Conversation{}, err
	}
	if a != userID && b != userID {
		return Conversation{}, ErrForbidden
	}

	blocked, err := s.gate.IsBlocked(ctx, a, b)
	if err != nil {
		return Conversation{}, err
	}
	if blocked {
		return Conversation{}, ErrBlocked
	}

	convID, err := s.store.createOrGet(ctx, a, b, &matchID)
	if err != nil {
		return Conversation{}, err
	}
	return s.store.conversationForUser(ctx, convID, userID)
}

// OpenByUser is only reachable when the match gate is disabled.
func (s *Service) OpenByUser(ctx context.Context, userID, peerID uuid.UUID) (Conversation, error) {
	if s.cfg.MatchGateEnabled {
		return Conversation{}, ErrGateRequired
	}
	if peerID == userID {
		return Conversation{}, ErrForbidden
	}

	blocked, err := s.gate.IsBlocked(ctx, userID, peerID)
	if err != nil {
		return Conversation{}, err
	}
	if blocked {
		return Conversation{}, ErrBlocked
	}

	convID, err := s.store.createOrGet(ctx, userID, peerID, nil)
	if err != nil {
		return Conversation{}, err
	}
	return s.store.conversationForUser(ctx, convID, userID)
}

func (s *Service) ListConversations(ctx context.Context, userID uuid.UUID, after *cursor.Keyset, limit int) ([]Conversation, *string, error) {
	items, err := s.store.listConversations(ctx, userID, after, limit+1)
	if err != nil {
		return nil, nil, err
	}

	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		activity := last.CreatedAt
		if last.LastMessageAt != nil {
			activity = *last.LastMessageAt
		}
		encoded := cursor.Encode(cursor.Keyset{Time: activity, ID: last.ID})
		next = &encoded
	}
	return items, next, nil
}

func (s *Service) ListMessages(ctx context.Context, userID, convID uuid.UUID, before *cursor.Keyset, limit int) ([]Message, *string, error) {
	if _, err := s.authorise(ctx, convID, userID); err != nil {
		return nil, nil, err
	}

	items, err := s.store.listMessages(ctx, convID, before, limit+1)
	if err != nil {
		return nil, nil, err
	}

	// Pages are ordered oldest to newest, so the extra row and the cursor both
	// sit at the start of the slice.
	var next *string
	if len(items) > limit {
		items = items[1:]
		oldest := items[0]
		encoded := cursor.Encode(cursor.Keyset{Time: oldest.CreatedAt, ID: oldest.ID})
		next = &encoded
	}
	return items, next, nil
}

// Send stores a message. The returned flag reports whether a new message was
// created, so a replayed client_msg_id can answer 200 instead of 201.
func (s *Service) Send(ctx context.Context, userID, convID uuid.UUID, content, clientMsgID string) (Message, bool, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Message{}, false, ErrEmptyContent
	}
	if len([]rune(content)) > s.cfg.MaxMessageLength {
		return Message{}, false, ErrContentTooLong
	}

	conv, err := s.authorise(ctx, convID, userID)
	if err != nil {
		return Message{}, false, err
	}

	msg, created, err := s.store.insertMessage(ctx, convID, userID, content, clientMsgID)
	if err != nil || !created {
		return msg, created, err
	}

	s.publishMessageCreated(ctx, conv, msg)
	return msg, created, nil
}

// publishMessageCreated is best effort: a delivered message must not fail
// because the queue is unavailable.
func (s *Service) publishMessageCreated(ctx context.Context, conv conversationRow, msg Message) {
	if s.pub == nil {
		return
	}

	recipient := conv.User1ID
	if recipient == msg.SenderID {
		recipient = conv.User2ID
	}

	_, err := s.pub.Publish(ctx, events.StreamNotifications, events.TypeMessageCreated, events.MessageCreated{
		ConversationID: conv.ID,
		MessageID:      msg.ID,
		SenderID:       msg.SenderID,
		RecipientID:    recipient,
		Preview:        preview(msg.Content),
	})
	if err != nil && s.log != nil {
		s.log.Warn("publishing message.created failed",
			"conversation_id", conv.ID, "message_id", msg.ID, "error", err)
	}
}

func (s *Service) MarkRead(ctx context.Context, userID, convID uuid.UUID) error {
	if _, err := s.authorise(ctx, convID, userID); err != nil {
		return err
	}
	return s.store.markRead(ctx, convID, userID)
}

// authorise checks participation and blocks, returning the conversation so
// callers do not have to load it again. A block closes an existing thread
// rather than only preventing new ones.
func (s *Service) authorise(ctx context.Context, convID, userID uuid.UUID) (conversationRow, error) {
	row, err := s.store.getConversation(ctx, convID)
	if err != nil {
		return conversationRow{}, err
	}
	if !row.hasParticipant(userID) {
		return conversationRow{}, ErrForbidden
	}

	blocked, err := s.gate.IsBlocked(ctx, row.User1ID, row.User2ID)
	if err != nil {
		return conversationRow{}, err
	}
	if blocked {
		return conversationRow{}, ErrBlocked
	}
	return row, nil
}
