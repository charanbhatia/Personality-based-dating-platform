package messaging

import (
	"context"
	"errors"
	"strings"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/cursor"
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

type Service struct {
	store *Store
	gate  *MatchGate
	cfg   Config
}

func NewService(store *Store, gate *MatchGate, cfg Config) *Service {
	return &Service{store: store, gate: gate, cfg: cfg}
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
	if err := s.assertParticipant(ctx, convID, userID); err != nil {
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
	if err := s.assertParticipant(ctx, convID, userID); err != nil {
		return Message{}, false, err
	}
	return s.store.insertMessage(ctx, convID, userID, content, clientMsgID)
}

func (s *Service) MarkRead(ctx context.Context, userID, convID uuid.UUID) error {
	if err := s.assertParticipant(ctx, convID, userID); err != nil {
		return err
	}
	return s.store.markRead(ctx, convID, userID)
}

// assertParticipant also blocks access once either user has blocked the other,
// so an existing thread goes quiet instead of staying open.
func (s *Service) assertParticipant(ctx context.Context, convID, userID uuid.UUID) error {
	row, err := s.store.getConversation(ctx, convID)
	if err != nil {
		return err
	}
	if !row.hasParticipant(userID) {
		return ErrForbidden
	}

	blocked, err := s.gate.IsBlocked(ctx, row.User1ID, row.User2ID)
	if err != nil {
		return err
	}
	if blocked {
		return ErrBlocked
	}
	return nil
}
