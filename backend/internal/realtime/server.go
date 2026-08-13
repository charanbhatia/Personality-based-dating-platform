package realtime

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/messaging"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/httpx"
	platformmw "github.com/bits-assignment/dating-platform/backend/internal/platform/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/ratelimit"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Config struct {
	AllowedOrigins  []string
	MaxMessageBytes int64
	PingInterval    time.Duration
	PongTimeout     time.Duration
	WriteTimeout    time.Duration
	JWTSecret       string

	SendLimit  int
	SendWindow time.Duration
}

type Server struct {
	hub       *Hub
	messaging *messaging.Service
	limiter   ratelimit.Limiter
	cfg       Config
	log       *slog.Logger
	upgrader  websocket.Upgrader
}

func NewServer(hub *Hub, svc *messaging.Service, limiter ratelimit.Limiter, cfg Config, log *slog.Logger) *Server {
	allowed := make(map[string]struct{}, len(cfg.AllowedOrigins))
	allowAll := false
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			allowAll = true
			continue
		}
		allowed[strings.ToLower(strings.TrimRight(o, "/"))] = struct{}{}
	}

	return &Server{
		hub:       hub,
		messaging: svc,
		limiter:   limiter,
		cfg:       cfg,
		log:       log,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				// Non-browser clients send no Origin header.
				if origin == "" || allowAll {
					return true
				}
				_, ok := allowed[strings.ToLower(strings.TrimRight(origin, "/"))]
				return ok
			},
		},
	}
}

// Handle upgrades the connection. Browsers cannot set headers on a WebSocket
// handshake, so the access token is also accepted as a query parameter.
func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.authenticate(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "unauthorized")
		return
	}
	platformmw.SetUserID(r.Context(), userID.String())

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote a response.
		s.log.Debug("websocket upgrade failed", "error", err)
		return
	}

	client := &Client{
		conn:   conn,
		hub:    s.hub,
		server: s,
		userID: userID,
		send:   make(chan []byte, sendBuffer),
	}

	s.hub.connections.Add(1)
	client.enqueue(encode(outbound{Type: TypeConnected}))

	go client.writeLoop()
	go func() {
		defer s.hub.connections.Add(-1)
		client.readLoop(context.WithoutCancel(r.Context()))
	}()
}

func (s *Server) authenticate(r *http.Request) (uuid.UUID, bool) {
	token := r.URL.Query().Get("access_token")
	if header := r.Header.Get("Authorization"); header != "" {
		if parts := strings.SplitN(header, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			token = parts[1]
		}
	}
	if token == "" {
		return uuid.Nil, false
	}

	userID, err := auth.ParseJWT(s.cfg.JWTSecret, token)
	if err != nil {
		return uuid.Nil, false
	}
	return userID, true
}

// allowSend applies the same per-user throttle as the REST send endpoint, so a
// client cannot bypass it by switching transport.
func (s *Server) allowSend(ctx context.Context, userID uuid.UUID) bool {
	if s.limiter == nil || s.cfg.SendLimit <= 0 {
		return true
	}

	res, err := s.limiter.Allow(ctx, "rl:msg:user:"+userID.String(), s.cfg.SendLimit, s.cfg.SendWindow)
	if err != nil {
		s.log.Warn("websocket rate limiter unavailable", "error", err)
		return true
	}
	return res.Allowed
}
