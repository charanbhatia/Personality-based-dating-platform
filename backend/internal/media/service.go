package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/events"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/storage"
	"github.com/google/uuid"
)

var (
	ErrForbidden          = errors.New("asset belongs to another user")
	ErrUnsupportedType    = errors.New("unsupported content type")
	ErrTooLarge           = errors.New("file exceeds the maximum upload size")
	ErrUploadMissing      = errors.New("no uploaded object found for this asset")
	ErrAlreadySubmitted   = errors.New("asset has already been submitted for processing")
	ErrStorageUnavailable = errors.New("media storage is not configured")
	ErrAssetNotReady      = errors.New("media asset is not ready")
)

type Publisher interface {
	Publish(ctx context.Context, stream, eventType string, payload any) (string, error)
}

type Config struct {
	MaxBytes         int64
	AllowedTypes     []string
	ThumbnailMaxEdge int
	PresignTTL       time.Duration
}

func (c Config) allows(contentType string) bool {
	for _, t := range c.AllowedTypes {
		if t == contentType {
			return true
		}
	}
	return false
}

type Service struct {
	store   *Store
	storage *storage.Client
	pub     Publisher
	cfg     Config
	log     *slog.Logger
}

func NewService(store *Store, s3 *storage.Client, pub Publisher, cfg Config, log *slog.Logger) *Service {
	return &Service{store: store, storage: s3, pub: pub, cfg: cfg, log: log}
}

type PresignResult struct {
	AssetID   uuid.UUID         `json:"asset_id"`
	UploadURL string            `json:"upload_url"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// Presign reserves an asset row and returns a URL the client PUTs the file to.
// The object is uploaded straight to storage; the API never proxies bytes.
func (s *Service) Presign(ctx context.Context, userID uuid.UUID, contentType string, byteSize int64) (PresignResult, error) {
	if s.storage == nil {
		return PresignResult{}, ErrStorageUnavailable
	}
	if !s.cfg.allows(contentType) {
		return PresignResult{}, ErrUnsupportedType
	}
	if byteSize <= 0 || byteSize > s.cfg.MaxBytes {
		return PresignResult{}, ErrTooLarge
	}

	assetID := uuid.New()
	key := originalKey(userID, assetID)

	url, err := s.storage.PresignPut(key, contentType, s.cfg.PresignTTL)
	if err != nil {
		return PresignResult{}, err
	}
	if _, err := s.store.create(ctx, assetID, userID, contentType, byteSize, key); err != nil {
		return PresignResult{}, err
	}

	return PresignResult{
		AssetID:   assetID,
		UploadURL: url,
		Headers:   map[string]string{"Content-Type": contentType},
		ExpiresAt: time.Now().UTC().Add(s.cfg.PresignTTL),
	}, nil
}

// Complete confirms the upload landed and queues derivative generation.
func (s *Service) Complete(ctx context.Context, userID, assetID uuid.UUID) (Asset, error) {
	asset, err := s.owned(ctx, userID, assetID)
	if err != nil {
		return Asset{}, err
	}
	if asset.Status != StatusPending {
		return asset, ErrAlreadySubmitted
	}

	exists, size, err := s.storage.Exists(ctx, asset.originalKey)
	if err != nil {
		return Asset{}, err
	}
	if !exists {
		return Asset{}, ErrUploadMissing
	}

	claimed, err := s.store.markProcessing(ctx, assetID, size)
	if err != nil {
		return Asset{}, err
	}
	if !claimed {
		return s.store.get(ctx, assetID)
	}

	if s.pub != nil {
		if _, err := s.pub.Publish(ctx, events.StreamMedia, events.TypeMediaProcess, events.MediaProcess{
			AssetID: assetID,
			UserID:  userID,
		}); err != nil {
			// The asset is already marked processing, so surface the failure
			// rather than leaving it stuck with no job queued.
			_ = s.store.markFailed(ctx, assetID, "could not queue processing")
			return Asset{}, err
		}
	}
	return s.store.get(ctx, assetID)
}

func (s *Service) Get(ctx context.Context, userID, assetID uuid.UUID) (Asset, error) {
	return s.owned(ctx, userID, assetID)
}

// ResolveAssetURLs turns owned, ready assets into public URLs in the given
// order. It is the profile gallery's media port.
func (s *Service) ResolveAssetURLs(ctx context.Context, userID uuid.UUID, assetIDs []uuid.UUID) ([]string, error) {
	urls := make([]string, 0, len(assetIDs))
	for _, id := range assetIDs {
		asset, err := s.Get(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		if asset.Status != StatusReady {
			return nil, ErrAssetNotReady
		}
		url := asset.OriginalURL
		if url == "" {
			url = asset.ThumbURL
		}
		if url == "" {
			return nil, ErrAssetNotReady
		}
		urls = append(urls, url)
	}
	return urls, nil
}

func (s *Service) Delete(ctx context.Context, userID, assetID uuid.UUID) error {
	asset, err := s.owned(ctx, userID, assetID)
	if err != nil {
		return err
	}

	// Objects are removed first; a failure here must not orphan the row.
	if s.storage != nil {
		if err := s.storage.Delete(ctx, asset.originalKey); err != nil {
			s.log.Warn("deleting original object failed", "asset_id", assetID, "error", err)
		}
		if asset.thumbKey != "" && asset.thumbKey != asset.originalKey {
			if err := s.storage.Delete(ctx, asset.thumbKey); err != nil {
				s.log.Warn("deleting thumbnail object failed", "asset_id", assetID, "error", err)
			}
		}
	}
	return s.store.delete(ctx, assetID)
}

// Process builds derivatives for an uploaded asset. It is the media worker's
// handler and must be safe to retry.
func (s *Service) Process(ctx context.Context, assetID uuid.UUID) error {
	if s.storage == nil {
		return ErrStorageUnavailable
	}

	asset, err := s.store.get(ctx, assetID)
	if err != nil {
		return err
	}
	if asset.Status == StatusReady {
		return nil
	}

	original, err := s.storage.Get(ctx, asset.originalKey)
	if err != nil {
		return fmt.Errorf("download original: %w", err)
	}

	thumbKey := asset.originalKey
	if Resizable(asset.ContentType) {
		thumb, err := Thumbnail(original, asset.ContentType, s.cfg.ThumbnailMaxEdge)
		if err != nil {
			// A corrupt image will never succeed, so fail it instead of
			// retrying until it dead letters.
			_ = s.store.markFailed(ctx, assetID, err.Error())
			s.log.Warn("thumbnail generation failed", "asset_id", assetID, "error", err)
			return nil
		}
		thumbKey = derivedKey(asset.UserID, assetID, "thumb")
		if err := s.storage.Put(ctx, thumbKey, ContentTypeJPEG, thumb); err != nil {
			return fmt.Errorf("upload thumbnail: %w", err)
		}
	}

	originalURL := s.storage.PublicURL(asset.originalKey)
	thumbURL := s.storage.PublicURL(thumbKey)
	if err := s.store.markReady(ctx, assetID, thumbKey, originalURL, thumbURL); err != nil {
		return err
	}

	if s.pub != nil {
		if _, err := s.pub.Publish(ctx, events.StreamMediaEvents, events.TypeMediaProcessed, events.MediaProcessed{
			UserID:      asset.UserID,
			AssetID:     assetID,
			OriginalURL: originalURL,
			ThumbURL:    thumbURL,
		}); err != nil {
			s.log.Warn("publishing media.processed failed", "asset_id", assetID, "error", err)
		}
	}
	return nil
}

func (s *Service) owned(ctx context.Context, userID, assetID uuid.UUID) (Asset, error) {
	asset, err := s.store.get(ctx, assetID)
	if err != nil {
		return Asset{}, err
	}
	if asset.UserID != userID {
		return Asset{}, ErrForbidden
	}
	return asset, nil
}

// publicPrefix is the only part of the bucket granted anonymous read, so
// browsers can render profile images directly while the rest stays private.
const publicPrefix = "public"

func originalKey(userID, assetID uuid.UUID) string {
	return derivedKey(userID, assetID, "original")
}

func derivedKey(userID, assetID uuid.UUID, name string) string {
	return fmt.Sprintf("%s/users/%s/%s/%s", publicPrefix, userID, assetID, name)
}
