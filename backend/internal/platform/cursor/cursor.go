// Package cursor encodes opaque pagination cursors shared by every domain.
package cursor

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrInvalid = errors.New("invalid cursor")

// Keyset positions a query on a (timestamp, id) pair so pagination stays stable
// when rows share a timestamp.
type Keyset struct {
	Time time.Time `json:"t"`
	ID   uuid.UUID `json:"id"`
}

func Encode(k Keyset) string {
	b, err := json.Marshal(k)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func Decode(raw string) (Keyset, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return Keyset{}, ErrInvalid
	}
	var k Keyset
	if err := json.Unmarshal(b, &k); err != nil || k.ID == uuid.Nil {
		return Keyset{}, ErrInvalid
	}
	return k, nil
}
