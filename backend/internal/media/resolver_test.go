package media

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/testdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestResolveAssetURLsRequiresOwnedReadyAssets(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := NewService(NewStore(pool), nil, nil, Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	owner := testdb.CreateUser(t, pool, "owner@example.com", "Owner")
	other := testdb.CreateUser(t, pool, "other@example.com", "Other")
	readyID := insertAsset(t, pool, owner, StatusReady, "https://cdn.example.test/ready.jpg")
	pendingID := insertAsset(t, pool, owner, StatusPending, "")
	foreignID := insertAsset(t, pool, other, StatusReady, "https://cdn.example.test/foreign.jpg")

	urls, err := svc.ResolveAssetURLs(ctx, owner, []uuid.UUID{readyID})
	if err != nil {
		t.Fatalf("ready asset: %v", err)
	}
	if len(urls) != 1 || urls[0] != "https://cdn.example.test/ready.jpg" {
		t.Fatalf("urls = %v, want the ready original URL", urls)
	}

	if _, err := svc.ResolveAssetURLs(ctx, owner, []uuid.UUID{pendingID}); !errors.Is(err, ErrAssetNotReady) {
		t.Errorf("pending asset = %v, want ErrAssetNotReady", err)
	}
	if _, err := svc.ResolveAssetURLs(ctx, owner, []uuid.UUID{foreignID}); !errors.Is(err, ErrForbidden) {
		t.Errorf("foreign asset = %v, want ErrForbidden", err)
	}
	if _, err := svc.ResolveAssetURLs(ctx, owner, []uuid.UUID{uuid.New()}); !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("missing asset = %v, want ErrAssetNotFound", err)
	}
}

func insertAsset(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, status, originalURL string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO media_assets (id, user_id, status, content_type, original_key, original_url, thumb_url)
		VALUES ($1, $2, $3, 'image/jpeg', $4, NULLIF($5, ''), NULLIF($5, ''))`,
		id, userID, status, "public/users/"+userID.String()+"/"+id.String()+"/original", originalURL)
	if err != nil {
		t.Fatalf("insert asset: %v", err)
	}
	return id
}
