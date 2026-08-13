// Package router wires HTTP routes onto the domain handlers.
//
// Person C owns routing conventions and the /api/v1 mount per the shared
// roadmap; each Person B domain contributes a RegisterRoutes function, so adding
// a C domain means one more registration call here rather than edits inside B's
// packages.
package router

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/config"
	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/handlers"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/bits-assignment/dating-platform/backend/internal/matching"
	"github.com/bits-assignment/dating-platform/backend/internal/media"
	"github.com/bits-assignment/dating-platform/backend/internal/messaging"
	"github.com/bits-assignment/dating-platform/backend/internal/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/notifications"
	"github.com/bits-assignment/dating-platform/backend/internal/personality"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/events"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/health"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/metrics"
	platformmw "github.com/bits-assignment/dating-platform/backend/internal/platform/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/queue"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/ratelimit"
	"github.com/bits-assignment/dating-platform/backend/internal/preferences"
	"github.com/bits-assignment/dating-platform/backend/internal/profile"
	"github.com/bits-assignment/dating-platform/backend/internal/realtime"
	"github.com/bits-assignment/dating-platform/backend/internal/repository"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

// Deps are the collaborators the router needs. Assembled by internal/app.
type Deps struct {
	Config *config.Config
	Pool   *pgxpool.Pool
	Tokens *auth.TokenManager
	Auth   *auth.Service
	Profile     *profile.Service
	Personality *personality.Service
	Preferences *preferences.Service
	Matching    *matching.Service

	// ProfileRepo backs the deprecated /api/matches shim.
	ProfileRepo *repository.ProfileRepo
	// Messaging is the pre-v1 conversation handler. Nil falls back to wiring
	// Person C's ConversationRepo when a pool is available.
	Messaging *handlers.MessagingHandler

	Redis   *goredis.Client
	Logger  *slog.Logger
	Context context.Context
}

// New builds the HTTP handler.
func New(deps Deps) (http.Handler, error) {
	cfg := deps.Config
	if cfg == nil {
		cfg = &config.Config{}
		deps.Config = cfg
	}
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	if deps.Context == nil {
		deps.Context = context.Background()
	}
	pool := deps.Pool
	if pool == nil {
		pool = db.Pool
	}

	tokens := deps.Tokens
	if tokens == nil && cfg.JWTSecret != "" {
		ttl := cfg.AccessTokenTTL
		if ttl <= 0 {
			ttl = 15 * time.Minute
		}
		tm, err := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTIssuer, ttl)
		if err != nil {
			return nil, err
		}
		tokens = tm
	}

	requireAuth := middleware.Auth(tokens)

	var limiter ratelimit.Limiter
	if cfg.RateLimitEnabled && deps.Redis != nil {
		limiter = ratelimit.NewRedisLimiter(deps.Redis)
	}
	authRateLimit := platformmw.RateLimit(platformmw.RateLimitConfig{
		Limiter:    limiter,
		Limit:      cfg.RateLimitAuth,
		Window:     cfg.RateLimitWindow,
		Scope:      "auth",
		FailClosed: true,
		KeyByEmail: true,
		Log:        log,
	})
	globalRateLimit := platformmw.RateLimit(platformmw.RateLimitConfig{
		Limiter: limiter,
		Limit:   cfg.RateLimitGlobal,
		Window:  cfg.RateLimitWindow,
		Scope:   "api",
		Log:     log,
	})

	r := mux.NewRouter()
	r.Use(routeLabeller)

	fallback := httpx.Wrap(func(w http.ResponseWriter, req *http.Request) error {
		allow := allowedMethods(r, req)
		if allow == "" {
			return httpx.NotFound("the requested endpoint does not exist")
		}
		w.Header().Set("Allow", allow)
		return httpx.CodedError(http.StatusMethodNotAllowed, httpx.CodeMethodNotAllowed,
			req.Method+" is not allowed on this endpoint")
	})
	r.NotFoundHandler = fallback
	r.MethodNotAllowedHandler = fallback

	healthHandler := &health.Handler{Pool: pool, Redis: deps.Redis}
	if cfg.MetricsEnabled {
		r.Handle("/metrics", metrics.Handler()).Methods(http.MethodGet)
	}
	r.HandleFunc("/healthz", healthHandler.Live).Methods(http.MethodGet)
	r.HandleFunc("/readyz", healthHandler.Ready).Methods(http.MethodGet)
	r.HandleFunc("/health", healthHandler.Live).Methods(http.MethodGet)

	v1 := r.PathPrefix("/api/v1").Subrouter()
	v1.NotFoundHandler = fallback
	v1.MethodNotAllowedHandler = fallback
	v1.Use(globalRateLimit)
	registerV1(v1, deps, requireAuth)

	v1Protected := v1.PathPrefix("").Subrouter()
	v1Protected.Use(requireAuth)

	var q *queue.Queue
	if deps.Redis != nil {
		q = queue.New(deps.Redis, log, cfg.QueueMaxLen)
	}
	var eventPub messaging.Publisher
	var mediaPub media.Publisher
	if q != nil {
		eventPub = q
		mediaPub = q
	}

	messagingSvc := messaging.NewService(
		messaging.NewStore(pool),
		messaging.NewMatchGate(pool),
		messaging.Config{
			MaxMessageLength: cfg.MaxMessageLength,
			MatchGateEnabled: cfg.MatchGateEnabled,
		},
		eventPub,
		log,
	)

	hub := realtime.NewHub(deps.Redis, log)
	go hub.Run(deps.Context)
	if err := metrics.RegisterWebSocketGauge(hub.Connections); err != nil {
		log.Warn("registering the websocket gauge failed", "error", err)
	}
	go metrics.SampleQueueDepth(deps.Context, deps.Redis, []string{
		events.StreamNotifications, events.StreamEmail, events.StreamMedia, events.StreamMediaEvents,
	}, cfg.MetricsSampleInterval)
	messagingSvc.WithBroadcaster(realtime.NewPublisher(deps.Redis, hub))

	wsServer := realtime.NewServer(hub, messagingSvc, limiter, realtime.Config{
		AllowedOrigins:  cfg.WSAllowedOrigins,
		MaxMessageBytes: cfg.WSMaxMessageBytes,
		PingInterval:    cfg.WSPingInterval,
		PongTimeout:     cfg.WSPongTimeout,
		WriteTimeout:    cfg.WSWriteTimeout,
		JWTSecret:       cfg.JWTSecret,
		SendLimit:       cfg.MessageSendLimit,
		SendWindow:      cfg.MessageSendWindow,
	}, log)
	r.HandleFunc("/ws", wsServer.Handle).Methods(http.MethodGet)

	messaging.NewHandler(
		messagingSvc,
		log,
		platformmw.RateLimit(platformmw.RateLimitConfig{
			Limiter:     limiter,
			Limit:       cfg.MessageSendLimit,
			Window:      cfg.MessageSendWindow,
			Scope:       "msg",
			KeyByUserID: true,
			Log:         log,
		}),
	).RegisterRoutes(v1Protected)

	mediaSvc, err := media.NewServiceFromConfig(pool, cfg, mediaPub, log)
	if err != nil {
		return nil, err
	}
	media.NewHandler(mediaSvc, log, platformmw.RateLimit(platformmw.RateLimitConfig{
		Limiter:     limiter,
		Limit:       cfg.MediaDailyLimit,
		Window:      24 * time.Hour,
		Scope:       "media",
		KeyByUserID: true,
		Log:         log,
	})).RegisterRoutes(v1Protected)

	notifications.NewHandler(
		notifications.NewServiceFromConfig(pool, deps.Redis, cfg, log),
		log,
	).RegisterRoutes(v1Protected)

	legacy := r.PathPrefix("/api").Subrouter()
	legacy.NotFoundHandler = fallback
	legacy.MethodNotAllowedHandler = fallback
	legacy.Use(globalRateLimit)
	registerLegacy(legacy, deps, pool, requireAuth, authRateLimit)

	var handler http.Handler = r
	handler = cors(cfg)(handler)
	handler = platformmw.Recover(log)(handler)
	handler = platformmw.Logger(log)(handler)
	handler = platformmw.Trace(handler)
	handler = platformmw.RequestID(handler)
	return handler, nil
}

func registerV1(api *mux.Router, deps Deps, requireAuth func(http.Handler) http.Handler) {
	auth.NewHandler(deps.Auth).RegisterRoutes(api, requireAuth)
	profile.NewHandler(deps.Profile).RegisterRoutes(api, requireAuth)
	personality.NewHandler(deps.Personality).RegisterRoutes(api, requireAuth)
	preferences.NewHandler(deps.Preferences).RegisterRoutes(api, requireAuth)
	matching.NewHandler(deps.Matching).RegisterRoutes(api, requireAuth)
}

func registerLegacy(api *mux.Router, deps Deps, pool *pgxpool.Pool, requireAuth func(http.Handler) http.Handler, authRateLimit func(http.Handler) http.Handler) {
	authHandler := &handlers.AuthHandler{Service: deps.Auth}
	meHandler := &handlers.MeHandler{Service: deps.Auth}
	profileHandler := &handlers.ProfileHandler{Service: deps.Profile}
	matchHandler := &handlers.MatchHandler{
		ProfileRepo:    deps.ProfileRepo,
		ProfileService: deps.Profile,
	}

	api.Handle("/auth/register", authRateLimit(httpx.Wrap(authHandler.Register))).Methods(http.MethodPost)
	api.Handle("/auth/login", authRateLimit(httpx.Wrap(authHandler.Login))).Methods(http.MethodPost)
	api.Handle("/auth/me", requireAuth(httpx.Wrap(meHandler.Me))).Methods(http.MethodGet)
	api.Handle("/profile", requireAuth(httpx.Wrap(profileHandler.Get))).Methods(http.MethodGet)
	api.Handle("/profile", requireAuth(httpx.Wrap(profileHandler.Update))).Methods(http.MethodPut)
	api.Handle("/matches", requireAuth(httpx.Wrap(matchHandler.List))).Methods(http.MethodGet)
	api.Handle("/matches/{id}", requireAuth(httpx.Wrap(matchHandler.Get))).Methods(http.MethodGet)

	messagingHandler := deps.Messaging
	if messagingHandler == nil && pool != nil {
		messagingHandler = &handlers.MessagingHandler{ConvRepo: repository.NewConversationRepo(pool)}
	}
	if messagingHandler == nil {
		messagingHandler = &handlers.MessagingHandler{}
	}
	api.Handle("/conversations",
		requireAuth(http.HandlerFunc(messagingHandler.ListConversations))).Methods(http.MethodGet)
	api.Handle("/conversations",
		requireAuth(http.HandlerFunc(messagingHandler.StartConversation))).Methods(http.MethodPost)
	api.Handle("/conversations/{id}/messages",
		requireAuth(http.HandlerFunc(messagingHandler.GetMessages))).Methods(http.MethodGet)
	api.Handle("/conversations/{id}/messages",
		requireAuth(http.HandlerFunc(messagingHandler.SendMessage))).Methods(http.MethodPost)
}

var probeMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost,
	http.MethodPut, http.MethodPatch, http.MethodDelete,
}

func allowedMethods(router *mux.Router, req *http.Request) string {
	allowed := make([]string, 0, len(probeMethods))
	for _, method := range probeMethods {
		if method == req.Method {
			continue
		}
		probe := req.Clone(req.Context())
		probe.Method = method
		var match mux.RouteMatch
		if router.Match(probe, &match) && match.MatchErr == nil {
			allowed = append(allowed, method)
		}
	}
	return strings.Join(allowed, ", ")
}

func corsOrigins(cfg *config.Config) []string {
	if len(cfg.CORSAllowedOrigins) > 0 {
		return cfg.CORSAllowedOrigins
	}
	return cfg.CORSOrigins
}

func cors(cfg *config.Config) func(http.Handler) http.Handler {
	origins := corsOrigins(cfg)
	allowed := make(map[string]struct{}, len(origins))
	allowAny := false
	for _, origin := range origins {
		if origin == "*" {
			allowAny = true
			continue
		}
		allowed[strings.ToLower(origin)] = struct{}{}
	}
	reflectAll := allowAny || len(allowed) == 0

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				_, listed := allowed[strings.ToLower(origin)]
				if reflectAll || listed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Add("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id, Idempotency-Key")
					w.Header().Set("Access-Control-Expose-Headers", "Deprecation, Link, X-Request-ID")
					w.Header().Set("Access-Control-Max-Age", "86400")
				}
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func routeLabeller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if route := mux.CurrentRoute(r); route != nil {
			if template, err := route.GetPathTemplate(); err == nil {
				platformmw.SetRoute(r.Context(), template)
			}
		}
		next.ServeHTTP(w, r)
	})
}
