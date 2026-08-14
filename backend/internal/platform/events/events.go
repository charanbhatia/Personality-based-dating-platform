// Package events holds the cross-domain event contracts. Both the identity and
// messaging sides publish and consume these, so payload changes must be agreed
// before the code changes.
package events

import (
	"time"

	"github.com/google/uuid"
)

// Stream names. Each is a Redis stream consumed by its own consumer group.
const (
	StreamNotifications = "queue:notifications"
	StreamEmail         = "queue:email"
	// StreamMedia carries work for the media worker; results are announced on
	// StreamMediaEvents so the worker does not consume its own output.
	StreamMedia       = "queue:media"
	StreamMediaEvents = "queue:media.events"
)

// Event types.
const (
	TypeMatchCreated               = "match.created"
	TypeUserBlocked                = "user.blocked"
	TypeMessageCreated             = "message.created"
	TypeMediaProcess               = "media.process"
	TypeMediaProcessed             = "media.processed"
	TypePasswordResetRequested     = "auth.password_reset_requested"
	TypeEmailVerificationRequested = "auth.email_verification_requested"
)

type MatchCreated struct {
	MatchID   uuid.UUID `json:"match_id"`
	UserAID   uuid.UUID `json:"user_a_id"`
	UserBID   uuid.UUID `json:"user_b_id"`
	CreatedAt time.Time `json:"created_at"`
}

type UserBlocked struct {
	BlockerID uuid.UUID `json:"blocker_id"`
	BlockedID uuid.UUID `json:"blocked_id"`
}

type MessageCreated struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	MessageID      uuid.UUID `json:"message_id"`
	SenderID       uuid.UUID `json:"sender_id"`
	RecipientID    uuid.UUID `json:"recipient_id"`
	Preview        string    `json:"preview"`
}

// MediaProcess is a job, not a fact: it asks the media worker to build
// derivatives for an uploaded asset.
type MediaProcess struct {
	AssetID uuid.UUID `json:"asset_id"`
	UserID  uuid.UUID `json:"user_id"`
}

type MediaProcessed struct {
	UserID      uuid.UUID `json:"user_id"`
	AssetID     uuid.UUID `json:"asset_id"`
	OriginalURL string    `json:"original_url"`
	ThumbURL    string    `json:"thumb_url"`
}

type PasswordResetRequested struct {
	UserID     uuid.UUID `json:"user_id"`
	Email      string    `json:"email"`
	ResetToken string    `json:"reset_token"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type EmailVerificationRequested struct {
	UserID      uuid.UUID `json:"user_id"`
	Email       string    `json:"email"`
	VerifyToken string    `json:"verify_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}
