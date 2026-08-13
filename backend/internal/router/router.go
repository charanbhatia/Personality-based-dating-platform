package router

import (
	"log/slog"
	"net/http"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/handlers"
	"github.com/bits-assignment/dating-platform/backend/internal/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/config"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/health"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/httpx"
	platformmw "github.com/bits-assignment/dating-platform/backend/internal/platform/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/ratelimit"
	"github.com/bits-assignment/dating-platform/backend/internal/repository"
	"github.com/gorilla/mux"
	goredis "github.com/redis/go-redis/v9"
)

type Deps struct {
	Config *config.Config
	Redis  *goredis.Client
	Logger *slog.Logger
}

func New(deps Deps) http.Handler {
	cfg := deps.Config
	log := deps.Logger
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
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "route not found")
	})
	r.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusMethodNotAllowed, httpx.CodeBadRequest, "method not allowed")
	})

	r.HandleFunc("/healthz", healthHandler.Live).Methods(http.MethodGet)
	r.HandleFunc("/readyz", healthHandler.Ready).Methods(http.MethodGet)
	// Retained so existing deployments and the current frontend keep working.
	r.HandleFunc("/health", healthHandler.Live).Methods(http.MethodGet)

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
	handler = platformmw.RequestID(handler)
	return handler
}
