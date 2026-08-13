package realtime

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

// Publisher broadcasts a stored message to every replica watching the
// conversation. It takes already-encoded JSON so the messaging domain does not
// need to know about the websocket protocol.
type Publisher struct {
	client *goredis.Client
	hub    *Hub
}

func NewPublisher(client *goredis.Client, hub *Hub) *Publisher {
	return &Publisher{client: client, hub: hub}
}

func (p *Publisher) PublishMessage(ctx context.Context, conversationID uuid.UUID, message json.RawMessage) error {
	payload := encode(outbound{
		Type:           TypeMessageNew,
		ConversationID: conversationID.String(),
		Message:        message,
	})

	// Without Redis the message is still delivered to clients on this process.
	if p.client == nil {
		p.hub.deliver(conversationID, payload)
		return nil
	}
	return p.client.Publish(ctx, channel(conversationID), payload).Err()
}
