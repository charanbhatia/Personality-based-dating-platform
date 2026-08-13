package router

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/handlers"
	"github.com/bits-assignment/dating-platform/backend/internal/media"
	"github.com/bits-assignment/dating-platform/backend/internal/messaging"
	"github.com/bits-assignment/dating-platform/backend/internal/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/notifications"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/config"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/events"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/health"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/httpx"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/metrics"
	platformmw "github.com/bits-assignment/dating-platform/backend/internal/platform/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/queue"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/ratelimit"
	"github.com/bits-assignment/dating-platform/backend/internal/realtime"
	"github.com/bits-assignment/dating-platform/backend/internal/repository"
	"github.com/gorilla/mux"
	goredis "github.com/redis/go-redis/v9"
)

type Deps struct {
	Config *config.Config
	Redis  *goredis.Client
	Logger *slog.Logger
	// Context bounds background work owned by the router, such as the
	// realtime hub's Redis subscription loop.
	Context context.Context
}

func New(deps Deps) (http.Handler, error) {
	cfg := deps.Config
	log := deps.Logger
	if deps.Context == nil {
		deps.Context = context.Background()
	}
	pool := db.Pool

	userRepo := repository.NewUserRepo(pool)
	profileRepo := repository.NewProfileRepo(pool)
	convRepo := repository.NewConversationRepo(pool)

	authHandler := &handlers.AuthHandler{
		UserRepo:    userRepo,
		ProfileRepo: profileRepo,
		JWTSecret:   cfg.JWTSecret,
	}
	meHandler := &handlers.MeHandler{UserRepo: userRepo}
	profileHandler := &handlers.ProfileHandler{ProfileRepo: profileRepo}
	matchHandler := &handlers.MatchHandler{ProfileRepo: profileRepo, UserRepo: userRepo}
	messagingHandler := &handlers.MessagingHandler{ConvRepo: convRepo}
	healthHandler := &health.Handler{Pool: pool, Redis: deps.Redis}

	authMiddleware := middleware.Auth(cfg.JWTSecret)

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
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "route not found")
	})
	r.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusMethodNotAllowed, httpx.CodeBadRequest, "method not allowed")
	})

	if cfg.MetricsEnabled {
		r.Handle("/metrics", metrics.Handler()).Methods(http.MethodGet)
	}

	r.HandleFunc("/healthz", healthHandler.Live).Methods(http.MethodGet)
	r.HandleFunc("/readyz", healthHandler.Ready).Methods(http.MethodGet)
	// Retained so existing deployments and the current frontend keep working.
	r.HandleFunc("/health", healthHandler.Live).Methods(http.MethodGet)

	// v1 is registered before the legacy prefix so /api/v1 never falls through
	// to the legacy catch-all subrouter.
	v1 := r.PathPrefix("/api/v1").Subrouter()
	v1.Use(globalRateLimit)
	v1Protected := v1.PathPrefix("").Subrouter()
	v1Protected.Use(authMiddleware)

	var publisher messaging.Publisher
	if deps.Redis != nil {
		publisher = queue.New(deps.Redis, log, cfg.QueueMaxLen)
	}

	messagingSvc := messaging.NewService(
		messaging.NewStore(pool),
		messaging.NewMatchGate(pool),
		messaging.Config{
			MaxMessageLength: cfg.MaxMessageLength,
			MatchGateEnabled: cfg.MatchGateEnabled,
		},
		publisher,
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

	messagingV1 := messaging.NewHandler(
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
	)
	messagingV1.RegisterRoutes(v1Protected)

	mediaSvc, err := media.NewServiceFromConfig(pool, cfg, publisher, log)
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

	api := r.PathPrefix("/api").Subrouter()
	api.Use(globalRateLimit)

	api.Handle("/auth/register", authRateLimit(http.HandlerFunc(authHandler.Register))).Methods(http.MethodPost)
	api.Handle("/auth/login", authRateLimit(http.HandlerFunc(authHandler.Login))).Methods(http.MethodPost)

	apiProtected := api.PathPrefix("").Subrouter()
	apiProtected.Use(authMiddleware)
	apiProtected.HandleFunc("/auth/me", meHandler.Me).Methods(http.MethodGet)
	apiProtected.HandleFunc("/profile", profileHandler.Get).Methods(http.MethodGet)
	apiProtected.HandleFunc("/profile", profileHandler.Update).Methods(http.MethodPut)
	apiProtected.HandleFunc("/matches", matchHandler.List).Methods(http.MethodGet)
	apiProtected.HandleFunc("/matches/{id}", matchHandler.Get).Methods(http.MethodGet)
	apiProtected.HandleFunc("/conversations", messagingHandler.ListConversations).Methods(http.MethodGet)
	apiProtected.HandleFunc("/conversations", messagingHandler.StartConversation).Methods(http.MethodPost)
	apiProtected.HandleFunc("/conversations/{id}/messages", messagingHandler.GetMessages).Methods(http.MethodGet)
	apiProtected.HandleFunc("/conversations/{id}/messages", messagingHandler.SendMessage).Methods(http.MethodPost)

	// CORS sits inside the observability wrappers but outside the mux so that
	// preflight requests are answered without depending on a route match.
	var handler http.Handler = r
	handler = platformmw.CORS(cfg.CORSOrigins)(handler)
	handler = platformmw.Recover(log)(handler)
	handler = platformmw.Logger(log)(handler)
	handler = platformmw.Trace(handler)
	handler = platformmw.RequestID(handler)
	return handler, nil
}

// routeLabeller records the matched path template so metrics label on
// /conversations/{id}/messages rather than one series per conversation.
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
