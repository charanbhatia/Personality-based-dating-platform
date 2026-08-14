package profile

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CodeMediaUnavailable is returned when a client submits media asset ids but
// the media service is not wired up.
const CodeMediaUnavailable = "media_unavailable"

// CodeAssetNotReady is returned when an uploaded asset has not finished processing.
const CodeAssetNotReady = "asset_not_ready"

// Pool is the database surface the service needs.
type Pool interface {
	db.Querier
	db.Beginner
}

// MediaResolver turns Person C's media asset ids into public URLs.
//
// Nil means PUT /profile/photos accepts HTTPS URLs only and rejects asset_ids
// with 501 media_unavailable.
type MediaResolver interface {
	ResolveAssetURLs(ctx context.Context, userID uuid.UUID, assetIDs []uuid.UUID) ([]string, error)
}

// Service implements profile use cases (F05, F06, F16).
type Service struct {
	pool  Pool
	repo  Repo
	media MediaResolver
	now   func() time.Time
}

type ServiceConfig struct {
	Pool  Pool
	Media MediaResolver
	Now   func() time.Time
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Pool == nil {
		return nil, errors.New("profile: pool is required")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{pool: cfg.Pool, media: cfg.Media, now: now}, nil
}

// Get returns the caller's own profile.
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (*DTO, error) {
	p, err := s.repo.Get(ctx, s.pool, userID)
	if err != nil {
		return nil, s.mapError(err, "load profile")
	}
	return s.toDTO(p), nil
}

// UpdateInput is a partial profile update. Nil fields are left unchanged.
type UpdateInput struct {
	Bio         *string
	Gender      *string
	Location    *string
	Lat         *float64
	Lng         *float64
	Interests   *[]string
	DateOfBirth *string
}

// Update validates and applies a partial profile update.
//
// Date of birth lives on users while the rest lives on profiles, so both writes
// share a transaction: a rejected age must not leave the bio updated.
func (s *Service) Update(ctx context.Context, userID uuid.UUID, in UpdateInput) (*DTO, error) {
	v := httpx.NewValidator()
	fields := UpdateFields{}

	if in.Bio != nil {
		bio := httpx.CleanText(*in.Bio)
		if httpx.TrimmedRuneLen(bio) > MaxBioRunes {
			v.Add("bio", fmt.Sprintf("must not exceed %d characters", MaxBioRunes))
		}
		fields.Bio = &bio
	}
	if in.Gender != nil {
		gender, ok := domain.NormalizeGender(*in.Gender)
		if !ok {
			v.Add("gender", "must be one of: "+strings.Join(domain.GenderStrings(), ", "))
		} else {
			value := string(gender)
			fields.Gender = &value
		}
	}
	if in.Location != nil {
		location := httpx.CleanLine(*in.Location)
		if httpx.TrimmedRuneLen(location) > MaxLocationRunes {
			v.Add("location", fmt.Sprintf("must not exceed %d characters", MaxLocationRunes))
		}
		fields.Location = &location
	}
	if in.Lat != nil || in.Lng != nil {
		if in.Lat == nil || in.Lng == nil {
			v.Add("lat", "lat and lng must be sent together")
		} else if *in.Lat < -90 || *in.Lat > 90 {
			v.Add("lat", "must be within [-90, 90]")
		} else if *in.Lng < -180 || *in.Lng > 180 {
			v.Add("lng", "must be within [-180, 180]")
		} else {
			fields.Lat = in.Lat
			fields.Lng = in.Lng
		}
	}
	if in.Interests != nil {
		interests, err := NormalizeInterests(*in.Interests)
		if err != nil {
			v.Add("interests", err.Error())
		} else {
			fields.Interests = &interests
		}
	}

	var dob *time.Time
	if in.DateOfBirth != nil {
		parsed, message := parseDOB(*in.DateOfBirth, s.now())
		if message != "" {
			v.Add("date_of_birth", message)
		} else {
			dob = parsed
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}

	var updated *Profile
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if dob != nil {
			if err := s.repo.SetDateOfBirth(ctx, tx, userID, *dob); err != nil {
				return err
			}
		}
		var err error
		updated, err = s.repo.Update(ctx, tx, userID, fields)
		return err
	})
	if err != nil {
		return nil, s.mapError(err, "update profile")
	}
	return s.toDTO(updated), nil
}

// SetPhotosInput carries either resolved URLs or Person C media asset ids.
type SetPhotosInput struct {
	PhotoURLs *[]string
	AssetIDs  *[]string
}

// SetPhotos replaces the photo gallery. Order is significant: the first entry
// becomes the primary photo used in the discovery feed.
func (s *Service) SetPhotos(ctx context.Context, userID uuid.UUID, in SetPhotosInput) (*DTO, error) {
	if in.PhotoURLs == nil && in.AssetIDs == nil {
		return nil, httpx.BadRequest("provide either photo_urls or asset_ids")
	}
	if in.PhotoURLs != nil && in.AssetIDs != nil {
		return nil, httpx.BadRequest("provide only one of photo_urls or asset_ids")
	}

	var urls []string
	switch {
	case in.AssetIDs != nil:
		resolved, err := s.resolveAssets(ctx, userID, *in.AssetIDs)
		if err != nil {
			return nil, err
		}
		urls = resolved
	default:
		validated, err := ValidatePhotoURLs(*in.PhotoURLs)
		if err != nil {
			v := httpx.NewValidator()
			v.Add("photo_urls", err.Error())
			return nil, v.Err()
		}
		urls = validated
	}

	updated, err := s.repo.SetPhotos(ctx, s.pool, userID, urls)
	if err != nil {
		return nil, s.mapError(err, "update photos")
	}
	return s.toDTO(updated), nil
}

func (s *Service) resolveAssets(ctx context.Context, userID uuid.UUID, rawIDs []string) ([]string, error) {
	if s.media == nil {
		return nil, httpx.NotImplemented(CodeMediaUnavailable,
			"media uploads are not enabled yet; submit photo_urls instead")
	}
	if len(rawIDs) > MaxPhotos {
		v := httpx.NewValidator()
		v.Add("asset_ids", fmt.Sprintf("must not contain more than %d entries", MaxPhotos))
		return nil, v.Err()
	}
	ids := make([]uuid.UUID, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			v := httpx.NewValidator()
			v.Add("asset_ids", "must contain valid UUIDs")
			return nil, v.Err()
		}
		ids = append(ids, id)
	}
	urls, err := s.media.ResolveAssetURLs(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	return ValidatePhotoURLs(urls)
}

// GetPublic returns another user's public card.
//
// A block in either direction is reported as 404 rather than 403: confirming that
// an account exists would let a blocked user keep tabs on the person who blocked
// them.
func (s *Service) GetPublic(ctx context.Context, viewerID, targetID uuid.UUID) (*PublicProfile, error) {
	if viewerID == targetID {
		own, err := s.repo.Get(ctx, s.pool, viewerID)
		if err != nil {
			return nil, s.mapError(err, "load profile")
		}
		return s.toPublic(own, false), nil
	}

	view, err := s.repo.GetPublic(ctx, s.pool, viewerID, targetID)
	if err != nil {
		return nil, s.mapError(err, "load public profile")
	}
	if view.IsBlocked {
		return nil, httpx.NotFound("user not found")
	}
	return s.toPublic(view.Profile, view.IsMatched), nil
}

// UpdateLegacy backs the pre-v1 PUT /api/profile, which sends the whole form and
// expects replace semantics including clearing a field.
func (s *Service) UpdateLegacy(ctx context.Context, userID uuid.UUID, bio, gender, location, photoURL string) (*DTO, error) {
	in := UpdateInput{Bio: &bio, Gender: &gender, Location: &location}
	updated, err := s.Update(ctx, userID, in)
	if err != nil {
		return nil, err
	}

	// The old endpoint carries a single photo. Mirror it into the gallery so both
	// representations agree, and treat an empty value as "clear".
	trimmed := strings.TrimSpace(photoURL)
	current := updated.PhotoURLs
	if trimmed == "" {
		if len(current) == 0 {
			return updated, nil
		}
		return s.SetPhotos(ctx, userID, SetPhotosInput{PhotoURLs: &[]string{}})
	}
	if len(current) > 0 && current[0] == trimmed {
		return updated, nil
	}
	replacement := append([]string{trimmed}, dropValue(current, trimmed)...)
	if len(replacement) > MaxPhotos {
		replacement = replacement[:MaxPhotos]
	}
	return s.SetPhotos(ctx, userID, SetPhotosInput{PhotoURLs: &replacement})
}

func dropValue(values []string, value string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v != value {
			out = append(out, v)
		}
	}
	return out
}

func (s *Service) toDTO(p *Profile) *DTO {
	dto := &DTO{
		UserID:          p.UserID,
		Name:            p.Name,
		Bio:             p.Bio,
		Gender:          p.Gender,
		Location:        p.Location,
		Lat:             p.Lat,
		Lng:             p.Lng,
		Interests:       nonNil(p.Interests),
		PhotoURLs:       nonNil(p.PhotoURLs),
		PrimaryPhotoURL: p.PrimaryPhotoURL,
		PhotoURL:        p.PrimaryPhotoURL,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
	if p.DateOfBirth != nil {
		formatted := p.DateOfBirth.Format(auth.DateLayout)
		dto.DateOfBirth = &formatted
		age := auth.AgeAt(*p.DateOfBirth, s.now())
		dto.Age = &age
	}
	return dto
}

func (s *Service) toPublic(p *Profile, isMatched bool) *PublicProfile {
	out := &PublicProfile{
		UserID:          p.UserID,
		Name:            p.Name,
		Bio:             p.Bio,
		Gender:          p.Gender,
		Location:        p.Location,
		Interests:       nonNil(p.Interests),
		PhotoURLs:       nonNil(p.PhotoURLs),
		PrimaryPhotoURL: p.PrimaryPhotoURL,
		IsMatched:       isMatched,
	}
	if p.DateOfBirth != nil {
		age := auth.AgeAt(*p.DateOfBirth, s.now())
		out.Age = &age
	}
	return out
}

func (s *Service) mapError(err error, action string) error {
	if errors.Is(err, ErrNotFound) {
		return httpx.NotFound("profile not found")
	}
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return httpx.Internal(fmt.Errorf("%s: %w", action, err))
}

// NormalizeInterests cleans, de-duplicates and bounds an interests list.
// Duplicates are matched case-insensitively but the user's own casing is kept.
func NormalizeInterests(raw []string) ([]string, error) {
	if len(raw) > MaxInterests {
		return nil, fmt.Errorf("must not contain more than %d interests", MaxInterests)
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		cleaned := httpx.CleanLine(item)
		if cleaned == "" {
			continue
		}
		if httpx.TrimmedRuneLen(cleaned) > MaxInterestRunes {
			return nil, fmt.Errorf("each interest must not exceed %d characters", MaxInterestRunes)
		}
		key := strings.ToLower(cleaned)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cleaned)
	}
	return out, nil
}

// ValidatePhotoURLs bounds and sanitises a gallery.
//
// Only HTTPS, loopback HTTP (for local development) and same-origin relative
// paths are accepted. Rejecting arbitrary schemes here is what stops a
// `javascript:` or `data:` URL from being stored and later rendered into an
// <img src> by the frontend.
func ValidatePhotoURLs(raw []string) ([]string, error) {
	if len(raw) > MaxPhotos {
		return nil, fmt.Errorf("must not contain more than %d photos", MaxPhotos)
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		candidate := strings.TrimSpace(item)
		if candidate == "" {
			continue
		}
		if len(candidate) > MaxPhotoURLLength {
			return nil, fmt.Errorf("each photo URL must not exceed %d characters", MaxPhotoURLLength)
		}
		if err := validatePhotoURL(candidate); err != nil {
			return nil, err
		}
		if _, dup := seen[candidate]; dup {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out, nil
}

func validatePhotoURL(candidate string) error {
	if strings.HasPrefix(candidate, "/") {
		// Relative path served by our own origin, e.g. /media/<asset>.jpg.
		if strings.HasPrefix(candidate, "//") {
			return errors.New("protocol-relative URLs are not allowed")
		}
		return nil
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return errors.New("each photo must be a valid URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "http":
		if !isLoopbackHost(parsed.Hostname()) {
			return errors.New("photo URLs must use https")
		}
	default:
		return errors.New("photo URLs must use https")
	}
	if parsed.Host == "" {
		return errors.New("each photo must be an absolute URL")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

func parseDOB(raw string, now time.Time) (*time.Time, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, "date_of_birth cannot be cleared once set"
	}
	parsed, err := time.Parse(auth.DateLayout, trimmed)
	if err != nil {
		return nil, "must be a date in YYYY-MM-DD format"
	}
	parsed = parsed.UTC()
	if parsed.After(now) {
		return nil, "must not be in the future"
	}
	age := auth.AgeAt(parsed, now)
	if age < auth.MinimumAge {
		return nil, fmt.Sprintf("you must be at least %d years old", auth.MinimumAge)
	}
	if age > auth.MaximumAge {
		return nil, "must be a realistic date of birth"
	}
	return &parsed, ""
}
