// Package router wires HTTP routes onto the domain handlers.
//
// Person C owns routing conventions and the /api/v1 mount per the shared
// roadmap; each Person B domain contributes a RegisterRoutes function, so adding
// a C domain means one more registration call here rather than edits inside B's
// packages.
package router

import (
	"net/http"
	"strings"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/config"
	"github.com/bits-assignment/dating-platform/backend/internal/handlers"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/bits-assignment/dating-platform/backend/internal/matching"
	"github.com/bits-assignment/dating-platform/backend/internal/personality"
	"github.com/bits-assignment/dating-platform/backend/internal/preferences"
	"github.com/bits-assignment/dating-platform/backend/internal/profile"
	"github.com/bits-assignment/dating-platform/backend/internal/repository"
	"github.com/gorilla/mux"
)

// Deps are the collaborators the router needs. Assembled by internal/app.
type Deps struct {
	Config      *config.Config
	Tokens      *auth.TokenManager
	Auth        *auth.Service
	Profile     *profile.Service
	Personality *personality.Service
	Preferences *preferences.Service
	Matching    *matching.Service

	// ProfileRepo backs the deprecated /api/matches shim.
	ProfileRepo *repository.ProfileRepo
	// Messaging is Person C's domain; nil until they wire it up.
	Messaging *handlers.MessagingHandler
}

// New builds the HTTP handler.
func New(deps Deps) http.Handler {
	requireAuth := auth.Middleware(deps.Tokens)

	r := mux.NewRouter()

	// One handler serves both fallbacks, and it decides between 404 and 405 by
	// re-matching the path under the other verbs.
	//
	// mux's own method-mismatch tracking cannot be trusted underneath a subrouter:
	// every subrouter route inherits the mount's path-prefix matcher, and a
	// successful matcher clears the recorded mismatch, so a route registered after
	// the one that mismatched erases it and the request surfaces as a 404. Asking
	// the routing table directly is order-independent and stays correct as routes
	// are added.
	fallback := httpx.Wrap(func(w http.ResponseWriter, req *http.Request) error {
		allow := allowedMethods(r, req)
		if allow == "" {
			return httpx.NotFound("the requested endpoint does not exist")
		}
		// RFC 9110 requires Allow on a 405, and it is what tells a client it used
		// the wrong verb rather than the wrong path.
		w.Header().Set("Allow", allow)
		return httpx.CodedError(http.StatusMethodNotAllowed, httpx.CodeMethodNotAllowed,
			req.Method+" is not allowed on this endpoint")
	})
	r.NotFoundHandler = fallback
	r.MethodNotAllowedHandler = fallback

	r.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}).Methods(http.MethodGet)

	// Each mount also gets the fallbacks, which mux does not inherit. This keeps a
	// mount authoritative for its prefix instead of letting an unmatched /api/v1
	// request fall through to the /api routes.
	v1 := r.PathPrefix("/api/v1").Subrouter()
	v1.NotFoundHandler = fallback
	v1.MethodNotAllowedHandler = fallback
	registerV1(v1, deps, requireAuth)

	legacy := r.PathPrefix("/api").Subrouter()
	legacy.NotFoundHandler = fallback
	legacy.MethodNotAllowedHandler = fallback
	registerLegacy(legacy, deps, requireAuth)

	// CORS wraps the whole mux rather than being registered with r.Use, because
	// mux does not run middleware for its NotFound and MethodNotAllowed handlers
	// and a browser needs the headers on those responses too.
	return cors(deps.Config)(r)
}

func registerV1(api *mux.Router, deps Deps, requireAuth func(http.Handler) http.Handler) {
	auth.NewHandler(deps.Auth).RegisterRoutes(api, requireAuth)
	profile.NewHandler(deps.Profile).RegisterRoutes(api, requireAuth)
	personality.NewHandler(deps.Personality).RegisterRoutes(api, requireAuth)
	preferences.NewHandler(deps.Preferences).RegisterRoutes(api, requireAuth)
	matching.NewHandler(deps.Matching).RegisterRoutes(api, requireAuth)
}

// registerLegacy mounts the pre-v1 endpoints the current SPA still calls. They
// delegate to the same services as /api/v1.
func registerLegacy(api *mux.Router, deps Deps, requireAuth func(http.Handler) http.Handler) {
	authHandler := &handlers.AuthHandler{Service: deps.Auth}
	meHandler := &handlers.MeHandler{Service: deps.Auth}
	profileHandler := &handlers.ProfileHandler{Service: deps.Profile}
	matchHandler := &handlers.MatchHandler{
		ProfileRepo:    deps.ProfileRepo,
		ProfileService: deps.Profile,
	}

	api.Handle("/auth/register", httpx.Wrap(authHandler.Register)).Methods(http.MethodPost)
	api.Handle("/auth/login", httpx.Wrap(authHandler.Login)).Methods(http.MethodPost)
	api.Handle("/auth/me", requireAuth(httpx.Wrap(meHandler.Me))).Methods(http.MethodGet)
	api.Handle("/profile", requireAuth(httpx.Wrap(profileHandler.Get))).Methods(http.MethodGet)
	api.Handle("/profile", requireAuth(httpx.Wrap(profileHandler.Update))).Methods(http.MethodPut)
	api.Handle("/matches", requireAuth(httpx.Wrap(matchHandler.List))).Methods(http.MethodGet)
	api.Handle("/matches/{id}", requireAuth(httpx.Wrap(matchHandler.Get))).Methods(http.MethodGet)

	if deps.Messaging == nil {
		return
	}
	api.Handle("/conversations",
		requireAuth(http.HandlerFunc(deps.Messaging.ListConversations))).Methods(http.MethodGet)
	api.Handle("/conversations",
		requireAuth(http.HandlerFunc(deps.Messaging.StartConversation))).Methods(http.MethodPost)
	api.Handle("/conversations/{id}/messages",
		requireAuth(http.HandlerFunc(deps.Messaging.GetMessages))).Methods(http.MethodGet)
	api.Handle("/conversations/{id}/messages",
		requireAuth(http.HandlerFunc(deps.Messaging.SendMessage))).Methods(http.MethodPost)
}

// probeMethods are the verbs the API uses. OPTIONS is absent because the CORS
// wrapper answers it before routing.
var probeMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost,
	http.MethodPut, http.MethodPatch, http.MethodDelete,
}

// allowedMethods re-matches the request path under each verb and returns those
// that route somewhere, as a ready-to-send Allow value. An empty result means no
// verb serves this path, i.e. it is a genuine 404.
func allowedMethods(router *mux.Router, req *http.Request) string {
	allowed := make([]string, 0, len(probeMethods))
	for _, method := range probeMethods {
		if method == req.Method {
			continue
		}
		probe := req.Clone(req.Context())
		probe.Method = method
		var match mux.RouteMatch
		// MatchErr is nil only for a genuine route hit; the fallback handlers
		// leave ErrNotFound or ErrMethodMismatch behind.
		if router.Match(probe, &match) && match.MatchErr == nil {
			allowed = append(allowed, method)
		}
	}
	return strings.Join(allowed, ", ")
}

// cors answers preflights and sets the response headers.
//
// With no allowlist configured the request origin is reflected, which keeps local
// development frictionless. config.Load requires CORS_ALLOWED_ORIGINS to be set
// when APP_ENV is a deployed environment, so production cannot end up reflecting.
func cors(cfg *config.Config) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(cfg.CORSAllowedOrigins))
	allowAny := false
	for _, origin := range cfg.CORSAllowedOrigins {
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
					// Responses differ per origin, so caches must key on it.
					w.Header().Add("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id, Idempotency-Key")
					w.Header().Set("Access-Control-Expose-Headers", "Deprecation, Link")
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
