package matching

import (
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/profile"
	"github.com/google/uuid"
)

// Swipe actions.
const (
	ActionLike = "like"
	ActionPass = "pass"
)

// Report reasons, mirroring the CHECK constraint in 006_b_matching.sql.
var reportReasons = []string{
	"spam",
	"harassment",
	"inappropriate_content",
	"fake_profile",
	"underage",
	"other",
}

// MaxReportDetailRunes bounds the free-text field on a report.
const MaxReportDetailRunes = 1000

// DiscoverItem is one card in the compatibility-sorted feed. It embeds the public
// profile shape, so no candidate payload can carry an email (roadmap F16).
type DiscoverItem struct {
	profile.PublicProfile
	CompatibilityScore float64 `json:"compatibility_score"`
}

// Page is the cursor-paginated envelope from roadmap §6.
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// SwipeResult is the response to a like or pass.
type SwipeResult struct {
	OK      bool       `json:"ok"`
	Action  string     `json:"action"`
	Matched bool       `json:"matched"`
	MatchID *uuid.UUID `json:"match_id"`
	// Duplicate reports that this swipe was already recorded, so the client can
	// tell an idempotent replay from a new decision.
	Duplicate bool `json:"duplicate"`
}

// MatchItem is one entry in the match list.
type MatchItem struct {
	MatchID uuid.UUID             `json:"match_id"`
	User    profile.PublicProfile `json:"user"`
	// CompatibilityScore is the unweighted similarity frozen when the match was
	// created; null for matches created before the score column existed.
	CompatibilityScore *float64  `json:"compatibility_score"`
	CreatedAt          time.Time `json:"created_at"`
}

// BlockItem is one entry in the block list.
type BlockItem struct {
	UserID    uuid.UUID `json:"user_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// ReportResult acknowledges a submitted report.
type ReportResult struct {
	ID uuid.UUID `json:"id"`
	// Duplicate is true when an open report for this pair already existed and was
	// returned instead of creating another.
	Duplicate bool `json:"duplicate"`
}

// ReportReasons returns the accepted reason codes.
func ReportReasons() []string {
	out := make([]string, len(reportReasons))
	copy(out, reportReasons)
	return out
}

func isValidReportReason(reason string) bool {
	for _, r := range reportReasons {
		if r == reason {
			return true
		}
	}
	return false
}
