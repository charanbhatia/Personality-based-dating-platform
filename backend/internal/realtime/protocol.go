// Package realtime implements the WebSocket chat gateway. Delivery is fanned
// out through Redis pub/sub so any API replica can reach any connected client.
package realtime

import (
	"encoding/json"

	"github.com/google/uuid"
)

// Client to server frame types.
const (
	TypeSubscribe   = "subscribe"
	TypeUnsubscribe = "unsubscribe"
	TypeMessageSend = "message.send"
	TypePing        = "ping"
)

// Server to client frame types.
const (
	TypeSubscribed = "subscribed"
	TypeMessageAck = "message.ack"
	TypeMessageNew = "message.new"
	TypeError      = "error"
	TypePong       = "pong"
	TypeConnected  = "connected"
)

// Error codes carried on TypeError frames.
const (
	CodeBadFrame    = "bad_frame"
	CodeForbidden   = "forbidden"
	CodeNotFound    = "not_found"
	CodeRateLimited = "rate_limited"
	CodeInvalid     = "invalid"
	CodeInternal    = "internal_error"
)

type inbound struct {
	Type           string `json:"type"`
	ConversationID string `json:"conversation_id"`
	Content        string `json:"content"`
	ClientMsgID    string `json:"client_msg_id"`
}

type outbound struct {
	Type           string          `json:"type"`
	ConversationID string          `json:"conversation_id,omitempty"`
	ClientMsgID    string          `json:"client_msg_id,omitempty"`
	Message        json.RawMessage `json:"message,omitempty"`
	Code           string          `json:"code,omitempty"`
	Error          string          `json:"error,omitempty"`
}

func encode(o outbound) []byte {
	b, err := json.Marshal(o)
	if err != nil {
		return []byte(`{"type":"error","code":"internal_error","error":"encoding failed"}`)
	}
	return b
}

func errorFrame(code, message string) []byte {
	return encode(outbound{Type: TypeError, Code: code, Error: message})
}

func channel(conversationID uuid.UUID) string {
	return "chat:conv:" + conversationID.String()
}
