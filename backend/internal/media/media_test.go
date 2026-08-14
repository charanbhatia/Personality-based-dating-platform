package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/storage"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/testdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// capturingPublisher records queued jobs instead of touching Redis.
type capturingPublisher struct {
	streams  []string
	types    []string
	payloads []any
}

func (p *capturingPublisher) Publish(_ context.Context, stream, eventType string, payload any) (string, error) {
	p.streams = append(p.streams, stream)
	p.types = append(p.types, eventType)
	p.payloads = append(p.payloads, payload)
	return uuid.NewString(), nil
}

func testService(t *testing.T) (*Service, *pgxpool.Pool, *capturingPublisher) {
	t.Helper()

	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_S3_ENDPOINT not set; skipping media integration test")
	}

	pool := testdb.New(t)
	client, err := storage.New(storage.Config{
		Endpoint:      endpoint,
		Region:        "us-east-1",
		Bucket:        envOr("TEST_S3_BUCKET", "dating-media"),
		AccessKey:     envOr("TEST_S3_ACCESS_KEY", "minioadmin"),
		SecretKey:     envOr("TEST_S3_SECRET_KEY", "minioadmin"),
		PublicBaseURL: endpoint,
		PathStyle:     true,
		PresignTTL:    5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("storage client: %v", err)
	}

	pub := &capturingPublisher{}
	svc := NewService(NewStore(pool), client, pub, Config{
		MaxBytes:         10 << 20,
		AllowedTypes:     []string{ContentTypeJPEG, ContentTypePNG, ContentTypeWebP},
		ThumbnailMaxEdge: 128,
		PresignTTL:       5 * time.Minute,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	return svc, pool, pub
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// upload performs the browser side of the flow: a plain PUT to the presigned URL.
func upload(t *testing.T, url, contentType string, body []byte) int {
	t.Helper()

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build upload request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		t.Logf("upload response: %s: %s", resp.Status, payload)
	}
	return resp.StatusCode
}

func TestFullUploadFlowProducesAThumbnail(t *testing.T) {
	svc, pool, pub := testService(t)
	ctx := context.Background()
	user := testdb.CreateUser(t, pool, "up@example.com", "Uploader")

	image := sampleImage(t, 900, 300, ContentTypeJPEG)

	presigned, err := svc.Presign(ctx, user, ContentTypeJPEG, int64(len(image)))
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if presigned.Headers["Content-Type"] != ContentTypeJPEG {
		t.Errorf("headers = %v, want the signed Content-Type", presigned.Headers)
	}

	if code := upload(t, presigned.UploadURL, ContentTypeJPEG, image); code != http.StatusOK {
		t.Fatalf("upload to presigned URL returned %d, want 200", code)
	}

	asset, err := svc.Complete(ctx, user, presigned.AssetID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if asset.Status != StatusProcessing {
		t.Errorf("status = %q, want %q", asset.Status, StatusProcessing)
	}
	if len(pub.types) != 1 || pub.types[0] != "media.process" {
		t.Fatalf("queued jobs = %v, want one media.process", pub.types)
	}

	if err := svc.Process(ctx, presigned.AssetID); err != nil {
		t.Fatalf("process: %v", err)
	}

	ready, err := svc.Get(ctx, user, presigned.AssetID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ready.Status != StatusReady {
		t.Fatalf("status = %q (%s), want ready", ready.Status, ready.Error)
	}
	if ready.ThumbURL == "" || ready.ThumbURL == ready.OriginalURL {
		t.Errorf("thumb URL %q must differ from the original %q", ready.ThumbURL, ready.OriginalURL)
	}

	// The generated thumbnail must be a real, smaller image.
	thumb, err := svc.storage.Get(ctx, derivedKey(user, presigned.AssetID, "thumb"))
	if err != nil {
		t.Fatalf("download thumbnail: %v", err)
	}
	b := decodeBounds(t, thumb)
	if b.Dx() != 128 || b.Dy() != 42 {
		t.Errorf("thumbnail = %dx%d, want 128x42", b.Dx(), b.Dy())
	}

	if len(pub.types) != 2 || pub.types[1] != "media.processed" {
		t.Errorf("events = %v, want media.processed after processing", pub.types)
	}
}

func TestWebPUploadsReuseTheOriginalAsThumbnail(t *testing.T) {
	svc, pool, _ := testService(t)
	ctx := context.Background()
	user := testdb.CreateUser(t, pool, "webp@example.com", "WebP User")

	// Contents are irrelevant: nothing decodes webp, it is stored as-is.
	body := []byte("RIFF....WEBPVP8 fake payload")

	presigned, err := svc.Presign(ctx, user, ContentTypeWebP, int64(len(body)))
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if code := upload(t, presigned.UploadURL, ContentTypeWebP, body); code != http.StatusOK {
		t.Fatalf("upload returned %d, want 200", code)
	}
	if _, err := svc.Complete(ctx, user, presigned.AssetID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := svc.Process(ctx, presigned.AssetID); err != nil {
		t.Fatalf("process: %v", err)
	}

	asset, err := svc.Get(ctx, user, presigned.AssetID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if asset.Status != StatusReady {
		t.Fatalf("status = %q (%s), want ready", asset.Status, asset.Error)
	}
	if asset.ThumbURL != asset.OriginalURL {
		t.Errorf("thumb %q should fall back to the original %q", asset.ThumbURL, asset.OriginalURL)
	}
}

func TestCompleteRejectsAnAssetWithNoUploadedObject(t *testing.T) {
	svc, pool, _ := testService(t)
	ctx := context.Background()
	user := testdb.CreateUser(t, pool, "lazy@example.com", "Lazy")

	presigned, err := svc.Presign(ctx, user, ContentTypeJPEG, 1024)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}

	if _, err := svc.Complete(ctx, user, presigned.AssetID); !errors.Is(err, ErrUploadMissing) {
		t.Errorf("complete without upload = %v, want ErrUploadMissing", err)
	}
}

func TestPresignValidatesTypeAndSize(t *testing.T) {
	svc, pool, _ := testService(t)
	ctx := context.Background()
	user := testdb.CreateUser(t, pool, "picky@example.com", "Picky")

	if _, err := svc.Presign(ctx, user, "application/pdf", 1024); !errors.Is(err, ErrUnsupportedType) {
		t.Errorf("pdf = %v, want ErrUnsupportedType", err)
	}
	if _, err := svc.Presign(ctx, user, ContentTypeJPEG, (10<<20)+1); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversized = %v, want ErrTooLarge", err)
	}
	if _, err := svc.Presign(ctx, user, ContentTypeJPEG, 0); !errors.Is(err, ErrTooLarge) {
		t.Errorf("zero bytes = %v, want ErrTooLarge", err)
	}
}

func TestAssetsAreScopedToTheirOwner(t *testing.T) {
	svc, pool, _ := testService(t)
	ctx := context.Background()
	owner := testdb.CreateUser(t, pool, "owner@example.com", "Owner")
	stranger := testdb.CreateUser(t, pool, "stranger@example.com", "Stranger")

	presigned, err := svc.Presign(ctx, owner, ContentTypeJPEG, 1024)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}

	if _, err := svc.Get(ctx, stranger, presigned.AssetID); !errors.Is(err, ErrForbidden) {
		t.Errorf("get = %v, want ErrForbidden", err)
	}
	if _, err := svc.Complete(ctx, stranger, presigned.AssetID); !errors.Is(err, ErrForbidden) {
		t.Errorf("complete = %v, want ErrForbidden", err)
	}
	if err := svc.Delete(ctx, stranger, presigned.AssetID); !errors.Is(err, ErrForbidden) {
		t.Errorf("delete = %v, want ErrForbidden", err)
	}
}

func TestDeleteRemovesTheAsset(t *testing.T) {
	svc, pool, _ := testService(t)
	ctx := context.Background()
	user := testdb.CreateUser(t, pool, "gone@example.com", "Gone")

	image := sampleImage(t, 200, 200, ContentTypeJPEG)
	presigned, err := svc.Presign(ctx, user, ContentTypeJPEG, int64(len(image)))
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	upload(t, presigned.UploadURL, ContentTypeJPEG, image)

	if err := svc.Delete(ctx, user, presigned.AssetID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Get(ctx, user, presigned.AssetID); !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("get after delete = %v, want ErrAssetNotFound", err)
	}

	exists, _, err := svc.storage.Exists(ctx, originalKey(user, presigned.AssetID))
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if exists {
		t.Error("the stored object should have been removed with the asset")
	}
}
