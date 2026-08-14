package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/cursor"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("notification not found")

type Notification struct {
	ID        uuid.UUID       `json:"id"`
	UserID    uuid.UUID       `json:"user_id"`
	Type      string          `json:"type"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	Data      json.RawMessage `json:"data"`
	ReadAt    *time.Time      `json:"read_at"`
	CreatedAt time.Time       `json:"created_at"`
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type newNotification struct {
	UserID  uuid.UUID
	Type    string
	Title   string
	Body    string
	Data    any
	EventID string
}

// insert is idempotent per (user_id, event_id): a redelivered event updates
// nothing and reports that no row was created.
func (s *Store) insert(ctx context.Context, n newNotification) (bool, error) {
	data, err := json.Marshal(n.Data)
	if err != nil {
		return false, err
	}

	const q = `INSERT INTO notifications (user_id, type, title, body, data, event_id)
	           VALUES ($1, $2, $3, $4, $5, $6)
	           ON CONFLICT (user_id, event_id) WHERE event_id IS NOT NULL DO NOTHING`

	tag, err := s.pool.Exec(ctx, q, n.UserID, n.Type, n.Title, n.Body, data, nullable(n.EventID))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) list(ctx context.Context, userID uuid.UUID, before *cursor.Keyset, limit int) ([]Notification, error) {
	const q = `SELECT id, user_id, type, title, body, data, read_at, created_at
	           FROM notifications
	           WHERE user_id = $1
	             AND ($2::timestamptz IS NULL OR (created_at, id) < ($2::timestamptz, $3::uuid))
	           ORDER BY created_at DESC, id DESC
	           LIMIT $4`

	var beforeTime *time.Time
	var beforeID *uuid.UUID
	if before != nil {
		beforeTime, beforeID = &before.Time, &before.ID
	}

	rows, err := s.pool.Query(ctx, q, userID, beforeTime, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Type, &n.Title, &n.Body, &n.Data, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) countUnread(ctx context.Context, userID uuid.UUID) (int64, error) {
	const q = `SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`

	var n int64
	err := s.pool.QueryRow(ctx, q, userID).Scan(&n)
	return n, err
}

// markRead reports whether the row changed, so an already-read notification
// does not decrement the unread counter twice.
func (s *Store) markRead(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	const q = `UPDATE notifications SET read_at = now()
	           WHERE id = $1 AND user_id = $2 AND read_at IS NULL`

	tag, err := s.pool.Exec(ctx, q, id, userID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	return false, s.exists(ctx, userID, id)
}

func (s *Store) markAllRead(ctx context.Context, userID uuid.UUID) error {
	const q = `UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`

	_, err := s.pool.Exec(ctx, q, userID)
	return err
}

func (s *Store) exists(ctx context.Context, userID, id uuid.UUID) error {
	const q = `SELECT 1 FROM notifications WHERE id = $1 AND user_id = $2`

	var x int
	if err := s.pool.QueryRow(ctx, q, id, userID).Scan(&x); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// userName resolves a display name for notification copy.
func (s *Store) userName(ctx context.Context, userID uuid.UUID) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM users WHERE id = $1`, userID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return name, err
}

// userEmail is only used for outbound mail.
func (s *Store) userEmail(ctx context.Context, userID uuid.UUID) (string, error) {
	var email string
	err := s.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return email, err
}

func (s *Store) upsertDevice(ctx context.Context, userID uuid.UUID, token, platform string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO device_tokens (user_id, token, platform)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, token) DO UPDATE
		SET platform = EXCLUDED.platform, updated_at = now()`, userID, token, platform)
	return err
}

func (s *Store) deleteDevice(ctx context.Context, userID uuid.UUID, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM device_tokens WHERE user_id = $1 AND token = $2`, userID, token)
	return err
}

func (s *Store) listDevices(ctx context.Context, userID uuid.UUID) ([]struct {
	Token    string
	Platform string
}, error) {
	rows, err := s.pool.Query(ctx, `SELECT token, platform FROM device_tokens WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		Token    string
		Platform string
	}
	for rows.Next() {
		var row struct {
			Token    string
			Platform string
		}
		if err := rows.Scan(&row.Token, &row.Platform); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
