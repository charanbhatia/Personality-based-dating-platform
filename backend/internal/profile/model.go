package profile

import (
	"time"

	"github.com/google/uuid"
)

// Limits on profile content. Kept here so validation, the API reference and the
// frontend form all trace back to one place.
const (
	MaxBioRunes       = 500
	MaxLocationRunes  = 120
	MaxNameRunes      = 120
	MaxInterests      = 20
	MaxInterestRunes  = 40
	MaxPhotos         = 9
	MaxPhotoURLLength = 1024
)

// Profile is the stored profile record.
type Profile struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	Bio             string
	Gender          string
	Location        string
	Interests       []string
	PhotoURLs       []string
	PrimaryPhotoURL string
	CreatedAt       time.Time
	UpdatedAt       time.Time

	// Denormalized from users for the response payloads.
	Name        string
	DateOfBirth *time.Time
}

// DTO is the owner's view of their own profile.
type DTO struct {
	UserID          uuid.UUID `json:"user_id"`
	Name            string    `json:"name"`
	Bio             string    `json:"bio"`
	Gender          string    `json:"gender"`
	Location        string    `json:"location"`
	Interests       []string  `json:"interests"`
	PhotoURLs       []string  `json:"photo_urls"`
	PrimaryPhotoURL string    `json:"primary_photo_url"`
	// PhotoURL is the pre-v1 single-photo field, kept in sync with PhotoURLs[0]
	// so the existing frontend keeps working while it migrates to the gallery.
	PhotoURL    string    `json:"photo_url"`
	DateOfBirth *string   `json:"date_of_birth"`
	Age         *int      `json:"age,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PublicProfile is what any other user may see.
//
// It deliberately has no email field: the pre-v1 match payload leaked addresses
// (roadmap F16), and keeping the public shape a distinct type makes that
// regression impossible to reintroduce by accident.
type PublicProfile struct {
	UserID          uuid.UUID `json:"user_id"`
	Name            string    `json:"name"`
	Age             *int      `json:"age,omitempty"`
	Bio             string    `json:"bio"`
	Gender          string    `json:"gender"`
	Location        string    `json:"location"`
	Interests       []string  `json:"interests"`
	PhotoURLs       []string  `json:"photo_urls"`
	PrimaryPhotoURL string    `json:"primary_photo_url"`
	IsMatched       bool      `json:"is_matched"`
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
