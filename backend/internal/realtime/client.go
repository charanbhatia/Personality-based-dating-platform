package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/messaging"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const sendBuffer = 32

type Client struct {
	conn   *websocket.Conn
	hub    *Hub
	server *Server
	userID uuid.UUID

	send      chan []byte
	closeOnce sync.Once
}

func (c *Client) enqueue(payload []byte) bool {
	select {
	case c.send <- payload:
		return true
	default:
		return false
	}
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.send)
		_ = c.conn.Close()
	})
}

// readLoop consumes client frames until the socket closes.
func (c *Client) readLoop(ctx context.Context) {
	defer func() {
		c.hub.leaveAll(context.WithoutCancel(ctx), c)
		c.close()
	}()

	cfg := c.server.cfg
	c.conn.SetReadLimit(cfg.MaxMessageBytes)
	_ = c.conn.SetReadDeadline(time.Now().Add(cfg.PongTimeout))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(cfg.PongTimeout))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}

		var frame inbound
		if err := json.Unmarshal(raw, &frame); err != nil {
			c.enqueue(errorFrame(CodeBadFrame, "frame is not valid json"))
			continue
		}
		c.handle(ctx, frame)
	}
}

func (c *Client) handle(ctx context.Context, frame inbound) {
	switch frame.Type {
	case TypePing:
		c.enqueue(encode(outbound{Type: TypePong}))

	case TypeSubscribe:
		convID, ok := c.parseConversation(frame)
		if !ok {
			return
		}
		if err := c.server.messaging.CanAccess(ctx, c.userID, convID); err != nil {
			c.enqueue(serviceError(err))
			return
		}
		if err := c.hub.Join(ctx, convID, c); err != nil {
			c.server.log.Error("websocket subscribe failed", "conversation_id", convID, "error", err)
			c.enqueue(errorFrame(CodeInternal, "could not subscribe"))
			return
		}
		c.enqueue(encode(outbound{Type: TypeSubscribed, ConversationID: convID.String()}))

	case TypeUnsubscribe:
		convID, ok := c.parseConversation(frame)
		if !ok {
			return
		}
		c.hub.Leave(ctx, convID, c)

	case TypeMessageSend:
		convID, ok := c.parseConversation(frame)
		if !ok {
			return
		}
		if !c.server.allowSend(ctx, c.userID) {
			c.enqueue(errorFrame(CodeRateLimited, "too many messages, slow down"))
			return
		}

		// The messaging service owns persistence, event publishing and the
		// broadcast, so a websocket send behaves exactly like a REST send.
		msg, _, err := c.server.messaging.Send(ctx, c.userID, convID, frame.Content, frame.ClientMsgID)
		if err != nil {
			c.enqueue(serviceError(err))
			return
		}

		encoded, err := json.Marshal(msg)
		if err != nil {
			c.enqueue(errorFrame(CodeInternal, "could not encode message"))
			return
		}
		c.enqueue(encode(outbound{
			Type:        TypeMessageAck,
			ClientMsgID: frame.ClientMsgID,
			Message:     encoded,
		}))

	default:
		c.enqueue(errorFrame(CodeBadFrame, "unknown frame type "+frame.Type))
	}
}

// writeLoop owns all writes to the socket, including keepalive pings.
func (c *Client) writeLoop() {
	cfg := c.server.cfg
	ticker := time.NewTicker(cfg.PingInterval)
	defer func() {
		ticker.Stop()
		c.close()
	}()

	for {
		select {
		case payload, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(cfg.WriteTimeout))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(cfg.WriteTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) parseConversation(frame inbound) (uuid.UUID, bool) {
	convID, err := uuid.Parse(frame.ConversationID)
	if err != nil {
		c.enqueue(errorFrame(CodeInvalid, "conversation_id must be a uuid"))
		return uuid.Nil, false
	}
	return convID, true
}

func serviceError(err error) []byte {
	switch {
	case errors.Is(err, messaging.ErrForbidden), errors.Is(err, messaging.ErrBlocked):
		return errorFrame(CodeForbidden, err.Error())
	case errors.Is(err, messaging.ErrConversationNotFound):
		return errorFrame(CodeNotFound, "conversation not found")
	case errors.Is(err, messaging.ErrEmptyContent), errors.Is(err, messaging.ErrContentTooLong):
		return errorFrame(CodeInvalid, err.Error())
	default:
		return errorFrame(CodeInternal, "internal server error")
	}
}
