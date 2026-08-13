package realtime

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

// Hub tracks which local connections are watching which conversation, and
// mirrors that set onto Redis subscriptions so a message published by any
// replica reaches every interested client.
type Hub struct {
	redis  *goredis.Client
	pubsub *goredis.PubSub
	log    *slog.Logger

	mu    sync.RWMutex
	rooms map[uuid.UUID]map[*Client]struct{}

	connections atomic.Int64
}

func NewHub(client *goredis.Client, log *slog.Logger) *Hub {
	return &Hub{
		redis: client,
		log:   log,
		rooms: make(map[uuid.UUID]map[*Client]struct{}),
	}
}

// Connections is the current number of open sockets on this process.
func (h *Hub) Connections() int64 { return h.connections.Load() }

// Run relays Redis messages to local subscribers until ctx is cancelled. With
// no Redis configured the hub still works, but only within one process.
func (h *Hub) Run(ctx context.Context) {
	if h.redis == nil {
		h.log.Warn("realtime running without redis; delivery is limited to this process")
		<-ctx.Done()
		return
	}

	h.pubsub = h.redis.Subscribe(ctx)
	defer h.pubsub.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-h.pubsub.Channel():
			if !ok {
				return
			}
			convID, err := conversationFromChannel(msg.Channel)
			if err != nil {
				continue
			}
			h.deliver(convID, []byte(msg.Payload))
		}
	}
}

// Join adds a client to a conversation, subscribing this process to the
// conversation's Redis channel on the first local watcher.
func (h *Hub) Join(ctx context.Context, convID uuid.UUID, c *Client) error {
	h.mu.Lock()
	room, exists := h.rooms[convID]
	if !exists {
		room = make(map[*Client]struct{})
		h.rooms[convID] = room
	}
	room[c] = struct{}{}
	first := !exists
	h.mu.Unlock()

	if first && h.pubsub != nil {
		if err := h.pubsub.Subscribe(ctx, channel(convID)); err != nil {
			h.Leave(ctx, convID, c)
			return err
		}
	}
	return nil
}

// Leave removes a client, dropping the Redis subscription once nobody local
// is watching the conversation.
func (h *Hub) Leave(ctx context.Context, convID uuid.UUID, c *Client) {
	h.mu.Lock()
	room, ok := h.rooms[convID]
	if !ok {
		h.mu.Unlock()
		return
	}
	delete(room, c)
	empty := len(room) == 0
	if empty {
		delete(h.rooms, convID)
	}
	h.mu.Unlock()

	if empty && h.pubsub != nil {
		if err := h.pubsub.Unsubscribe(ctx, channel(convID)); err != nil {
			h.log.Warn("unsubscribing from conversation failed", "conversation_id", convID, "error", err)
		}
	}
}

func (h *Hub) leaveAll(ctx context.Context, c *Client) {
	h.mu.RLock()
	rooms := make([]uuid.UUID, 0, len(h.rooms))
	for convID, room := range h.rooms {
		if _, ok := room[c]; ok {
			rooms = append(rooms, convID)
		}
	}
	h.mu.RUnlock()

	for _, convID := range rooms {
		h.Leave(ctx, convID, c)
	}
}

// deliver pushes a payload to every local client in a conversation. A client
// whose buffer is full is disconnected rather than allowed to block the hub.
func (h *Hub) deliver(convID uuid.UUID, payload []byte) {
	h.mu.RLock()
	room := h.rooms[convID]
	targets := make([]*Client, 0, len(room))
	for c := range room {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		if !c.enqueue(payload) {
			h.log.Warn("dropping slow websocket client", "user_id", c.userID)
			c.close()
		}
	}
}

func conversationFromChannel(name string) (uuid.UUID, error) {
	const prefix = "chat:conv:"
	if len(name) <= len(prefix) {
		return uuid.Nil, uuid.Validate(name)
	}
	return uuid.Parse(name[len(prefix):])
}
