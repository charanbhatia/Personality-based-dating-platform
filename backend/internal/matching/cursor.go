package matching

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/google/uuid"
)

// cursorVersion is embedded in every cursor so a future change to the pagination
// key can reject stale cursors explicitly instead of mis-decoding them.
const cursorVersion = 1

// discoverCursor is the keyset position in the compatibility-sorted feed.
//
// The score travels as a decimal string rather than a float so it round-trips
// exactly. The feed orders by (score DESC, user_id ASC) and the score is rounded
// server-side, which makes the equality half of the keyset comparison reliable —
// with a float64 that comparison would intermittently skip or repeat rows.
type discoverCursor struct {
	Version int       `json:"v"`
	Score   string    `json:"s"`
	UserID  uuid.UUID `json:"u"`
}

// matchCursor is the keyset position in the match list, ordered by
// (created_at DESC, id DESC).
type matchCursor struct {
	Version   int       `json:"v"`
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"i"`
}

// decimalPattern guards the score before it reaches the query. The value is
// bound as a parameter so injection is not the concern; an unparseable string
// would fail the numeric cast and surface as a 500 instead of a 400.
var decimalPattern = regexp.MustCompile(`^(0|1)(\.[0-9]{1,10})?$`)

func encodeCursor(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeCursor(raw string, dst any) error {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return httpx.BadRequest("cursor is not valid")
	}
	if err := json.Unmarshal(decoded, dst); err != nil {
		return httpx.BadRequest("cursor is not valid")
	}
	return nil
}

// parseDiscoverCursor decodes and validates a discovery cursor.
func parseDiscoverCursor(raw string) (*discoverCursor, error) {
	if raw == "" {
		return nil, nil
	}
	var c discoverCursor
	if err := decodeCursor(raw, &c); err != nil {
		return nil, err
	}
	if c.Version != cursorVersion || c.UserID == uuid.Nil || !decimalPattern.MatchString(c.Score) {
		return nil, httpx.BadRequest("cursor is not valid")
	}
	if score, err := strconv.ParseFloat(c.Score, 64); err != nil || score < 0 || score > 1 {
		return nil, httpx.BadRequest("cursor is not valid")
	}
	return &c, nil
}

// parseMatchCursor decodes and validates a match-list cursor.
func parseMatchCursor(raw string) (*matchCursor, error) {
	if raw == "" {
		return nil, nil
	}
	var c matchCursor
	if err := decodeCursor(raw, &c); err != nil {
		return nil, err
	}
	if c.Version != cursorVersion || c.ID == uuid.Nil || c.CreatedAt.IsZero() {
		return nil, httpx.BadRequest("cursor is not valid")
	}
	return &c, nil
}
