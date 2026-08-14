package push

import (
	"context"

	"github.com/google/uuid"
)

// Note is one notification payload delivered to a device.
type Note struct {
	Title string
	Body  string
	Data  map[string]string
}

// Token is a registered device endpoint.
type Token struct {
	UserID   uuid.UUID
	Token    string
	Platform string
}

// Pusher delivers a note to a single device token.
type Pusher interface {
	Push(ctx context.Context, token Token, note Note) error
}

// Nop logs nothing and always succeeds. Used when no vendor credentials are set.
type Nop struct{}

func (Nop) Push(context.Context, Token, Note) error { return nil }

// Multi tries each vendor in order. A platform mismatch is skipped, not failed.
type Multi struct {
	Vendors []Pusher
}

func (m Multi) Push(ctx context.Context, token Token, note Note) error {
	var last error
	tried := false
	for _, v := range m.Vendors {
		if v == nil {
			continue
		}
		tried = true
		if err := v.Push(ctx, token, note); err != nil {
			last = err
			continue
		}
		return nil
	}
	if !tried {
		return nil
	}
	return last
}
