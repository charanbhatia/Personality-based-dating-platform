package app

import (
	"context"
	"errors"

	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/bits-assignment/dating-platform/backend/internal/media"
	"github.com/bits-assignment/dating-platform/backend/internal/profile"
	"github.com/google/uuid"
)

// mediaURLResolver adapts Person C's media service onto the profile gallery port.
type mediaURLResolver struct {
	svc *media.Service
}

func (r mediaURLResolver) ResolveAssetURLs(ctx context.Context, userID uuid.UUID, assetIDs []uuid.UUID) ([]string, error) {
	urls, err := r.svc.ResolveAssetURLs(ctx, userID, assetIDs)
	if err != nil {
		return nil, mapMediaResolveError(err)
	}
	return urls, nil
}

func mapMediaResolveError(err error) error {
	switch {
	case errors.Is(err, media.ErrAssetNotFound), errors.Is(err, media.ErrForbidden):
		// Same 404 as a missing id, so guessing another user's asset id cannot
		// confirm that the upload exists.
		return httpx.NotFound("media asset not found")
	case errors.Is(err, media.ErrAssetNotReady):
		return httpx.Conflict(profile.CodeAssetNotReady, "this upload is not ready yet")
	default:
		return httpx.Internal(err)
	}
}
