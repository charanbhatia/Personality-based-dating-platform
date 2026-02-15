package router

import (
	"net/http"

	"github.com/bits-assignment/dating-platform/backend/internal/config"
	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/handlers"
	"github.com/bits-assignment/dating-platform/backend/internal/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/repository"
	"github.com/gorilla/mux"
)

func New(cfg *config.Config) http.Handler {
	pool := db.Pool
	userRepo := repository.NewUserRepo(pool)
	profileRepo := repository.NewProfileRepo(pool)
	convRepo := repository.NewConversationRepo(pool)

	authHandler := &handlers.AuthHandler{
		UserRepo:   userRepo,
		ProfileRepo: profileRepo,
		JWTSecret:  cfg.JWTSecret,
	}
	meHandler := &handlers.MeHandler{UserRepo: userRepo}
	profileHandler := &handlers.ProfileHandler{ProfileRepo: profileRepo}
	matchHandler := &handlers.MatchHandler{ProfileRepo: profileRepo, UserRepo: userRepo}
	messagingHandler := &handlers.MessagingHandler{ConvRepo: convRepo}

	authMiddleware := middleware.Auth(cfg.JWTSecret)

	r := mux.NewRouter()
	r.Use(corsMiddleware)
	r.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}).Methods(http.MethodGet)

	api := r.PathPrefix("/api").Subrouter()

	api.HandleFunc("/auth/register", authHandler.Register).Methods(http.MethodPost)
	api.HandleFunc("/auth/login", authHandler.Login).Methods(http.MethodPost)

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

	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
