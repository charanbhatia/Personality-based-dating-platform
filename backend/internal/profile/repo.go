package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/google/uuid"
)

// ErrNotFound is returned when no profile row exists for a user.
var ErrNotFound = errors.New("profile not found")

const selectColumns = `
	p.id, p.user_id, p.bio, p.gender, p.location, p.lat, p.lng, p.interests, p.photo_urls,
	coalesce(p.primary_photo_url, ''), p.created_at, p.updated_at,
	u.name, u.date_of_birth`

// Repo reads and writes the profiles table.
type Repo struct{}

func scan(row interface{ Scan(dest ...any) error }) (*Profile, error) {
	p := &Profile{}
	var photoURLs []byte
	err := row.Scan(&p.ID, &p.UserID, &p.Bio, &p.Gender, &p.Location, &p.Lat, &p.Lng, &p.Interests, &photoURLs,
		&p.PrimaryPhotoURL, &p.CreatedAt, &p.UpdatedAt, &p.Name, &p.DateOfBirth)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	urls, err := decodePhotoURLs(photoURLs)
	if err != nil {
		return nil, err
	}
	p.PhotoURLs = urls
	if p.Interests == nil {
		p.Interests = []string{}
	}
	return p, nil
}

// decodePhotoURLs tolerates non-string entries rather than failing the whole
// read, so a bad write elsewhere degrades one photo instead of the profile page.
func decodePhotoURLs(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	var values []any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decode photo_urls: %w", err)
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// Get loads a profile joined with the identity fields the DTOs need.
func (Repo) Get(ctx context.Context, q db.Querier, userID uuid.UUID) (*Profile, error) {
	return scan(q.QueryRow(ctx, `
		SELECT `+selectColumns+`
		FROM profiles p
		JOIN users u ON u.id = p.user_id
		WHERE p.user_id = $1`, userID))
}

// UpdateFields is the resolved set of changes to apply. A nil field is left
// untouched, which is what makes PUT behave as a merge and stops a client that
// omits `bio` from silently erasing it.
type UpdateFields struct {
	Bio       *string
	Gender    *string
	Location  *string
	Lat       *float64
	Lng       *float64
	Interests *[]string
}

// Update applies a partial profile update and returns the stored row.
func (r Repo) Update(ctx context.Context, q db.Querier, userID uuid.UUID, fields UpdateFields) (*Profile, error) {
	row := q.QueryRow(ctx, `
		UPDATE profiles p
		SET bio        = coalesce($2, p.bio),
		    gender     = coalesce($3, p.gender),
		    location   = coalesce($4, p.location),
		    interests  = coalesce($5::text[], p.interests),
		    lat        = coalesce($6, p.lat),
		    lng        = coalesce($7, p.lng),
		    updated_at = now()
		FROM users u
		WHERE p.user_id = $1 AND u.id = p.user_id
		RETURNING `+selectColumns,
		userID, fields.Bio, fields.Gender, fields.Location, arrayParam(fields.Interests), fields.Lat, fields.Lng)
	return scan(row)
}

// arrayParam converts an optional slice into a bind value, since a typed nil
// pointer would not encode as SQL NULL.
func arrayParam(values *[]string) any {
	if values == nil {
		return nil
	}
	return nonNil(*values)
}

// SetPhotos replaces the gallery, keeping primary_photo_url and the legacy
// photo_url column in sync with the first entry.
func (r Repo) SetPhotos(ctx context.Context, q db.Querier, userID uuid.UUID, urls []string) (*Profile, error) {
	encoded, err := json.Marshal(nonNil(urls))
	if err != nil {
		return nil, fmt.Errorf("encode photo_urls: %w", err)
	}
	var primary any
	if len(urls) > 0 {
		primary = urls[0]
	}
	// $3 is cast explicitly because it feeds both a text and a varchar column;
	// without it Postgres deduces conflicting types for the same parameter.
	row := q.QueryRow(ctx, `
		UPDATE profiles p
		SET photo_urls        = $2::jsonb,
		    primary_photo_url = $3::text,
		    photo_url         = coalesce($3::text, ''),
		    updated_at        = now()
		FROM users u
		WHERE p.user_id = $1 AND u.id = p.user_id
		RETURNING `+selectColumns,
		userID, encoded, primary)
	return scan(row)
}

// SetDateOfBirth updates the identity record. Date of birth lives on users but is
// edited through the profile form, so the profile service owns this write and
// performs it in the same transaction as the profile update.
func (Repo) SetDateOfBirth(ctx context.Context, q db.Querier, userID uuid.UUID, dob time.Time) error {
	tag, err := q.Exec(ctx,
		`UPDATE users SET date_of_birth = $2, updated_at = now() WHERE id = $1`, userID, dob)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PublicView is a profile as seen by another user, with the relationship flags
// resolved in the same query.
type PublicView struct {
	Profile   *Profile
	IsMatched bool
	IsBlocked bool
}

// GetPublic loads another user's profile along with whether the two are matched
// and whether either has blocked the other.
//
// Doing this in one statement keeps the visibility decision atomic; separate
// round trips could observe a block that lands between them and still return the
// profile.
func (Repo) GetPublic(ctx context.Context, q db.Querier, viewerID, targetID uuid.UUID) (*PublicView, error) {
	p := &Profile{}
	var photoURLs []byte
	view := &PublicView{}

	err := q.QueryRow(ctx, `
		SELECT `+selectColumns+`,
			EXISTS (
				SELECT 1 FROM matches m
				WHERE m.user_a_id = least($1::uuid, $2::uuid)
				  AND m.user_b_id = greatest($1::uuid, $2::uuid)
			) AS is_matched,
			EXISTS (
				SELECT 1 FROM blocks b
				WHERE (b.blocker_id = $1::uuid AND b.blocked_id = $2::uuid)
				   OR (b.blocker_id = $2::uuid AND b.blocked_id = $1::uuid)
			) AS is_blocked
		FROM profiles p
		JOIN users u ON u.id = p.user_id
		WHERE p.user_id = $2::uuid`, viewerID, targetID,
	).Scan(&p.ID, &p.UserID, &p.Bio, &p.Gender, &p.Location, &p.Lat, &p.Lng, &p.Interests, &photoURLs,
		&p.PrimaryPhotoURL, &p.CreatedAt, &p.UpdatedAt, &p.Name, &p.DateOfBirth,
		&view.IsMatched, &view.IsBlocked)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	urls, err := decodePhotoURLs(photoURLs)
	if err != nil {
		return nil, err
	}
	p.PhotoURLs = urls
	if p.Interests == nil {
		p.Interests = []string{}
	}
	view.Profile = p
	return view, nil
}
