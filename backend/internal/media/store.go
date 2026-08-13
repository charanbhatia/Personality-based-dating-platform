package media

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusReady      = "ready"
	StatusFailed     = "failed"
)

var ErrAssetNotFound = errors.New("media asset not found")

// Asset is the client-facing view of an upload. Object keys stay internal.
type Asset struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	Status      string    `json:"status"`
	ContentType string    `json:"content_type"`
	ByteSize    int64     `json:"byte_size"`
	OriginalURL string    `json:"original_url,omitempty"`
	ThumbURL    string    `json:"thumb_url,omitempty"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	originalKey string
	thumbKey    string
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const assetColumns = `id, user_id, status, content_type, COALESCE(byte_size, 0),
                      original_key, COALESCE(thumb_key, ''),
                      COALESCE(original_url, ''), COALESCE(thumb_url, ''),
                      COALESCE(error, ''), created_at, updated_at`

func scanAsset(row pgx.Row) (Asset, error) {
	var a Asset
	err := row.Scan(&a.ID, &a.UserID, &a.Status, &a.ContentType, &a.ByteSize,
		&a.originalKey, &a.thumbKey, &a.OriginalURL, &a.ThumbURL,
		&a.Error, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func (s *Store) create(ctx context.Context, id, userID uuid.UUID, contentType string, byteSize int64, originalKey string) (Asset, error) {
	const q = `INSERT INTO media_assets (id, user_id, status, content_type, byte_size, original_key)
	           VALUES ($1, $2, 'pending', $3, $4, $5)
	           RETURNING ` + assetColumns

	return scanAsset(s.pool.QueryRow(ctx, q, id, userID, contentType, byteSize, originalKey))
}

func (s *Store) get(ctx context.Context, id uuid.UUID) (Asset, error) {
	const q = `SELECT ` + assetColumns + ` FROM media_assets WHERE id = $1`

	a, err := scanAsset(s.pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrAssetNotFound
	}
	return a, err
}

// markProcessing only advances pending assets, so a duplicate complete call
// cannot re-queue work that is already running.
func (s *Store) markProcessing(ctx context.Context, id uuid.UUID, byteSize int64) (bool, error) {
	const q = `UPDATE media_assets
	           SET status = 'processing', byte_size = COALESCE(NULLIF($2, 0), byte_size), updated_at = now()
	           WHERE id = $1 AND status = 'pending'`

	tag, err := s.pool.Exec(ctx, q, id, byteSize)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) markReady(ctx context.Context, id uuid.UUID, thumbKey, originalURL, thumbURL string) error {
	const q = `UPDATE media_assets
	           SET status = 'ready', thumb_key = $2, original_url = $3, thumb_url = $4,
	               error = NULL, updated_at = now()
	           WHERE id = $1`

	_, err := s.pool.Exec(ctx, q, id, thumbKey, originalURL, thumbURL)
	return err
}

func (s *Store) markFailed(ctx context.Context, id uuid.UUID, reason string) error {
	const q = `UPDATE media_assets SET status = 'failed', error = $2, updated_at = now() WHERE id = $1`

	_, err := s.pool.Exec(ctx, q, id, reason)
	return err
}

func (s *Store) delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM media_assets WHERE id = $1`, id)
	return err
}
