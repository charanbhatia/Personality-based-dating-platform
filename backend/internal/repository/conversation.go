package repository

import (
	"context"

	"github.com/bits-assignment/dating-platform/backend/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ConversationRepo struct {
	pool *pgxpool.Pool
}

func NewConversationRepo(pool *pgxpool.Pool) *ConversationRepo {
	return &ConversationRepo{pool: pool}
}

func orderUserIDs(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	if a.String() < b.String() {
		return a, b
	}
	return b, a
}

func (r *ConversationRepo) CreateOrGet(ctx context.Context, user1ID, user2ID uuid.UUID) (*models.Conversation, error) {
	u1, u2 := orderUserIDs(user1ID, user2ID)
	q := `INSERT INTO conversations (user1_id, user2_id, created_at) VALUES ($1, $2, now())
	      ON CONFLICT (user1_id, user2_id) DO UPDATE SET user1_id = conversations.user1_id
	      RETURNING id, user1_id, user2_id, created_at`
	c := &models.Conversation{}
	err := r.pool.QueryRow(ctx, q, u1, u2).Scan(&c.ID, &c.User1ID, &c.User2ID, &c.CreatedAt)
	return c, err
}

func (r *ConversationRepo) ListByUserID(ctx context.Context, userID uuid.UUID) ([]models.Conversation, error) {
	q := `SELECT id, user1_id, user2_id, created_at FROM conversations WHERE user1_id = $1 OR user2_id = $1 ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Conversation{}
	for rows.Next() {
		var c models.Conversation
		err := rows.Scan(&c.ID, &c.User1ID, &c.User2ID, &c.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *ConversationRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.Conversation, error) {
	q := `SELECT id, user1_id, user2_id, created_at FROM conversations WHERE id = $1`
	c := &models.Conversation{}
	err := r.pool.QueryRow(ctx, q, id).Scan(&c.ID, &c.User1ID, &c.User2ID, &c.CreatedAt)
	return c, err
}

func (r *ConversationRepo) UserInConversation(ctx context.Context, conversationID, userID uuid.UUID) (bool, error) {
	q := `SELECT 1 FROM conversations WHERE id = $1 AND (user1_id = $2 OR user2_id = $2)`
	var x int
	err := r.pool.QueryRow(ctx, q, conversationID, userID).Scan(&x)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *ConversationRepo) Messages(ctx context.Context, conversationID uuid.UUID, limit, offset int) ([]models.Message, error) {
	q := `SELECT id, conversation_id, sender_id, content, created_at FROM messages
	      WHERE conversation_id = $1 ORDER BY created_at ASC LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, conversationID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Message{}
	for rows.Next() {
		var m models.Message
		err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Content, &m.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *ConversationRepo) SendMessage(ctx context.Context, conversationID, senderID uuid.UUID, content string) (*models.Message, error) {
	q := `INSERT INTO messages (id, conversation_id, sender_id, content, created_at) VALUES (gen_random_uuid(), $1, $2, $3, now())
	      RETURNING id, conversation_id, sender_id, content, created_at`
	m := &models.Message{}
	err := r.pool.QueryRow(ctx, q, conversationID, senderID, content).Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Content, &m.CreatedAt)
	return m, err
}
