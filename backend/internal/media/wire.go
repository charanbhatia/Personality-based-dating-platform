package media

import (
	"log/slog"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/config"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewServiceFromConfig builds the media service shared by the API and the
// worker. It returns a service with no storage client when object storage is
// not configured, so the endpoints report 503 instead of panicking.
func NewServiceFromConfig(pool *pgxpool.Pool, cfg *config.Config, pub Publisher, log *slog.Logger) (*Service, error) {
	var client *storage.Client
	if cfg.MediaEnabled() {
		c, err := storage.New(storage.Config{
			Endpoint:      cfg.S3Endpoint,
			Region:        cfg.S3Region,
			Bucket:        cfg.S3Bucket,
			AccessKey:     cfg.S3AccessKey,
			SecretKey:     cfg.S3SecretKey,
			PublicBaseURL: cfg.S3PublicBaseURL,
			PathStyle:     cfg.S3PathStyle,
			PresignTTL:    cfg.S3PresignTTL,
		})
		if err != nil {
			return nil, err
		}
		client = c
	}

	return NewService(NewStore(pool), client, pub, Config{
		MaxBytes:         cfg.MediaMaxBytes,
		AllowedTypes:     cfg.MediaAllowedTypes,
		ThumbnailMaxEdge: cfg.ThumbnailMaxEdge,
		PresignTTL:       cfg.S3PresignTTL,
	}, log), nil
}
