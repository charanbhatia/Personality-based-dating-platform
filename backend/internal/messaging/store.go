package messaging

import (
	"context"
	"errors"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/cursor"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrConversationNotFound = errors.New("conversation not found")

const previewRunes = 140

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type conversationRow struct {
	ID        uuid.UUID
	User1ID   uuid.UUID
	User2ID   uuid.UUID
	MatchID   *uuid.UUID
	CreatedAt time.Time
}

func (r conversationRow) hasParticipant(userID uuid.UUID) bool {
	return r.User1ID == userID || r.User2ID == userID
}

func (s *Store) getConversation(ctx context.Context, id uuid.UUID) (conversationRow, error) {
	const q = `SELECT id, user1_id, user2_id, match_id, created_at FROM conversations WHERE id = $1`

	var row conversationRow
	err := s.pool.QueryRow(ctx, q, id).Scan(&row.ID, &row.User1ID, &row.User2ID, &row.MatchID, &row.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return conversationRow{}, ErrConversationNotFound
	}
	return row, err
}

// createOrGet normalises participant order to satisfy the user1_id < user2_id
// constraint, and backfills match_id onto conversations opened before the
// mutual-match gate existed.
func (s *Store) createOrGet(ctx context.Context, a, b uuid.UUID, matchID *uuid.UUID) (uuid.UUID, error) {
	u1, u2 := a, b
	if u1.String() > u2.String() {
		u1, u2 = u2, u1
	}

	const q = `INSERT INTO conversations (user1_id, user2_id, match_id, created_at)
	           VALUES ($1, $2, $3, now())
	           ON CONFLICT (user1_id, user2_id)
	           DO UPDATE SET match_id = COALESCE(conversations.match_id, EXCLUDED.match_id)
	           RETURNING id`

	var id uuid.UUID
	err := s.pool.QueryRow(ctx, q, u1, u2, matchID).Scan(&id)
	return id, err
}

// conversationSelect resolves the peer participant and their unread count for
// the viewer bound to $1. It never selects peer.email.
const conversationSelect = `SELECT c.id,
                                   c.match_id,
                                   c.last_message_at,
                                   COALESCE(c.last_message_preview, ''),
                                   c.created_at,
                                   COALESCE(c.last_message_at, c.created_at) AS activity_at,
                                   peer.id,
                                   peer.name,
                                   COALESCE(p.photo_url, ''),
                                   COALESCE(p.bio, ''),
                                   COALESCE(p.location, ''),
                                   (SELECT COUNT(*) FROM messages m
                                      WHERE m.conversation_id = c.id
                                        AND m.sender_id <> $1
                                        AND (cr.last_read_at IS NULL OR m.created_at > cr.last_read_at))
                            FROM conversations c
                            JOIN users peer
                              ON peer.id = CASE WHEN c.user1_id = $1 THEN c.user2_id ELSE c.user1_id END
                            LEFT JOIN profiles p ON p.user_id = peer.id
                            LEFT JOIN conversation_reads cr ON cr.conversation_id = c.id AND cr.user_id = $1`

func scanConversation(row pgx.Row) (Conversation, error) {
	var c Conversation
	var activityAt time.Time
	err := row.Scan(
		&c.ID, &c.MatchID, &c.LastMessageAt, &c.LastMessagePreview, &c.CreatedAt, &activityAt,
		&c.Peer.UserID, &c.Peer.Name, &c.Peer.PhotoURL, &c.Peer.Bio, &c.Peer.Location,
		&c.UnreadCount,
	)
	return c, err
}

func (s *Store) listConversations(ctx context.Context, userID uuid.UUID, after *cursor.Keyset, limit int) ([]Conversation, error) {
	q := conversationSelect + `
	     WHERE (c.user1_id = $1 OR c.user2_id = $1)
	       AND NOT EXISTS (
	           SELECT 1 FROM blocks b
	           WHERE (b.blocker_id = c.user1_id AND b.blocked_id = c.user2_id)
	              OR (b.blocker_id = c.user2_id AND b.blocked_id = c.user1_id)
	       )
	       AND ($2::timestamptz IS NULL
	            OR (COALESCE(c.last_message_at, c.created_at), c.id) < ($2::timestamptz, $3::uuid))
	     ORDER BY activity_at DESC, c.id DESC
	     LIMIT $4`

	var afterTime *time.Time
	var afterID *uuid.UUID
	if after != nil {
		afterTime, afterID = &after.Time, &after.ID
	}

	rows, err := s.pool.Query(ctx, q, userID, afterTime, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Conversation{}
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) conversationForUser(ctx context.Context, convID, userID uuid.UUID) (Conversation, error) {
	q := conversationSelect + ` WHERE c.id = $2 AND (c.user1_id = $1 OR c.user2_id = $1)`

	c, err := scanConversation(s.pool.QueryRow(ctx, q, userID, convID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrConversationNotFound
	}
	return c, err
}

// listMessages returns the newest page first and walks backwards through
// history. Items within a page are ordered oldest to newest so a chat view can
// render them directly.
func (s *Store) listMessages(ctx context.Context, convID uuid.UUID, before *cursor.Keyset, limit int) ([]Message, error) {
	const q = `SELECT id, conversation_id, sender_id, content, COALESCE(client_msg_id, ''), created_at
	           FROM messages
	           WHERE conversation_id = $1
	             AND ($2::timestamptz IS NULL OR (created_at, id) < ($2::timestamptz, $3::uuid))
	           ORDER BY created_at DESC, id DESC
	           LIMIT $4`

	var beforeTime *time.Time
	var beforeID *uuid.UUID
	if before != nil {
		beforeTime, beforeID = &before.Time, &before.ID
	}

	rows, err := s.pool.Query(ctx, q, convID, beforeTime, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	descending := []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Content, &m.ClientMsgID, &m.CreatedAt); err != nil {
			return nil, err
		}
		descending = append(descending, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, j := 0, len(descending)-1; i < j; i, j = i+1, j-1 {
		descending[i], descending[j] = descending[j], descending[i]
	}
	return descending, nil
}

// insertMessage is idempotent on client_msg_id: a retry with the same id
// returns the message stored by the first attempt instead of duplicating it.
func (s *Store) insertMessage(ctx context.Context, convID, senderID uuid.UUID, content, clientMsgID string) (Message, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Message{}, false, err
	}
	defer tx.Rollback(ctx)

	var clientID *string
	if clientMsgID != "" {
		clientID = &clientMsgID
	}

	const insert = `INSERT INTO messages (id, conversation_id, sender_id, content, client_msg_id, created_at)
	                VALUES (gen_random_uuid(), $1, $2, $3, $4, now())
	                ON CONFLICT (conversation_id, sender_id, client_msg_id)
	                    WHERE client_msg_id IS NOT NULL
	                DO NOTHING
	                RETURNING id, conversation_id, sender_id, content, COALESCE(client_msg_id, ''), created_at`

	var m Message
	err = tx.QueryRow(ctx, insert, convID, senderID, content, clientID).
		Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Content, &m.ClientMsgID, &m.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		const existing = `SELECT id, conversation_id, sender_id, content, COALESCE(client_msg_id, ''), created_at
		                  FROM messages
		                  WHERE conversation_id = $1 AND sender_id = $2 AND client_msg_id = $3`
		if err := tx.QueryRow(ctx, existing, convID, senderID, clientMsgID).
			Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Content, &m.ClientMsgID, &m.CreatedAt); err != nil {
			return Message{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Message{}, false, err
		}
		return m, false, nil
	}
	if err != nil {
		return Message{}, false, err
	}

	const touch = `UPDATE conversations SET last_message_at = $2, last_message_preview = $3 WHERE id = $1`
	if _, err := tx.Exec(ctx, touch, convID, m.CreatedAt, preview(content)); err != nil {
		return Message{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Message{}, false, err
	}
	return m, true, nil
}

func (s *Store) markRead(ctx context.Context, convID, userID uuid.UUID) error {
	const q = `INSERT INTO conversation_reads (conversation_id, user_id, last_read_at)
	           VALUES ($1, $2, now())
	           ON CONFLICT (conversation_id, user_id) DO UPDATE SET last_read_at = now()`

	_, err := s.pool.Exec(ctx, q, convID, userID)
	return err
}

func preview(content string) string {
	runes := []rune(content)
	if len(runes) <= previewRunes {
		return content
	}
	return string(runes[:previewRunes]) + "…"
}
