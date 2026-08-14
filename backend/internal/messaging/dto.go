package messaging

import (
	"time"

	"github.com/google/uuid"
)

// Peer carries the public identity of the other participant. It deliberately
// omits email so conversation payloads never leak contact details.
type Peer struct {
	UserID   uuid.UUID `json:"user_id"`
	Name     string    `json:"name"`
	PhotoURL string    `json:"photo_url"`
	Bio      string    `json:"bio"`
	Location string    `json:"location"`
}

type Conversation struct {
	ID                 uuid.UUID  `json:"id"`
	MatchID            *uuid.UUID `json:"match_id"`
	Peer               Peer       `json:"peer"`
	LastMessagePreview string     `json:"last_message_preview"`
	LastMessageAt      *time.Time `json:"last_message_at"`
	UnreadCount        int        `json:"unread_count"`
	PeerLastReadAt     *time.Time `json:"peer_last_read_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

type Message struct {
	ID             uuid.UUID `json:"id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	SenderID       uuid.UUID `json:"sender_id"`
	Content        string    `json:"content"`
	ClientMsgID    string    `json:"client_msg_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}
