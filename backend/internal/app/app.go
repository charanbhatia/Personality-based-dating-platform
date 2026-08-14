// Package app assembles the domain services, HTTP handler and background workers.
//
// Keeping construction in one place means cmd/server, cmd/seed and the
// integration tests all build the same object graph, so a wiring mistake cannot
// exist only in tests or only in production.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/cache"
	"github.com/bits-assignment/dating-platform/backend/internal/config"
	"github.com/bits-assignment/dating-platform/backend/internal/matching"
	"github.com/bits-assignment/dating-platform/backend/internal/media"
	"github.com/bits-assignment/dating-platform/backend/internal/outbox"
	"github.com/bits-assignment/dating-platform/backend/internal/personality"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/queue"
	"github.com/bits-assignment/dating-platform/backend/internal/preferences"
	"github.com/bits-assignment/dating-platform/backend/internal/profile"
	"github.com/bits-assignment/dating-platform/backend/internal/repository"
	"github.com/bits-assignment/dating-platform/backend/internal/router"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

// sessionSweepInterval controls how often expired sessions are deleted.
const sessionSweepInterval = time.Hour

// Options are the pluggable collaborators Person C supplies. Every field is
// optional and falls back to a working default.
type Options struct {
	// Publisher delivers outbox events. Defaults to structured logging until the
	// Redis Streams publisher exists.
	Publisher outbox.Publisher
	// TraitCache caches Big Five vectors. Defaults to Redis when a client is
	// supplied, otherwise an in-process TTL cache.
	TraitCache cache.Cache
	// Media resolves upload asset ids to URLs. Nil means the app constructs
	// Person C's media service and wires it here.
	Media profile.MediaResolver
	// Redis is optional. When set, the router enables rate limits, queues and WS fanout.
	Redis *goredis.Client
	// Logger is passed to Person C's platform middleware. Nil uses slog.Default.
	Logger *slog.Logger
	// Now overrides the clock, for tests.
	Now func() time.Time
}

// App is a fully wired application.
type App struct {
	Config *config.Config
	Pool   *pgxpool.Pool

	Handler http.Handler

	Auth        *auth.Service
	Profile     *profile.Service
	Personality *personality.Service
	Preferences *preferences.Service
	Matching    *matching.Service

	Tokens  *auth.TokenManager
	Drainer *outbox.Drainer

	memoryCache *cache.Memory
}

// New builds the application graph.
func New(cfg *config.Config, pool *pgxpool.Pool, opts Options) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("app: config is required")
	}
	if pool == nil {
		return nil, fmt.Errorf("app: database pool is required")
	}

	tokens, err := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.AccessTokenTTL)
	if err != nil {
		return nil, err
	}

	drainer := outbox.NewDrainer(pool, opts.Publisher, cfg.OutboxBatchSize, cfg.OutboxPollInterval, cfg.OutboxMaxAttempts)

	traitCache := opts.TraitCache
	var memoryCache *cache.Memory
	if traitCache == nil {
		if opts.Redis != nil {
			traitCache = cache.NewRedis(opts.Redis)
		} else {
			memoryCache = cache.NewMemory(0)
			traitCache = memoryCache
		}
	}

	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	var mediaPub media.Publisher
	if opts.Redis != nil {
		mediaPub = queue.New(opts.Redis, log, cfg.QueueMaxLen)
	}
	mediaSvc, err := media.NewServiceFromConfig(pool, cfg, mediaPub, log)
	if err != nil {
		return nil, fmt.Errorf("media: %w", err)
	}

	mediaResolver := opts.Media
	if mediaResolver == nil {
		mediaResolver = mediaURLResolver{svc: mediaSvc}
	}

	authService, err := auth.NewService(auth.ServiceConfig{
		Pool:                 pool,
		Tokens:               tokens,
		RefreshTTL:           cfg.RefreshTokenTTL,
		PasswordResetTTL:     cfg.PasswordResetTTL,
		EmailVerificationTTL: cfg.EmailVerificationTTL,
		Kicker:               drainer,
		Now:                  opts.Now,
	})
	if err != nil {
		return nil, err
	}

	profileService, err := profile.NewService(profile.ServiceConfig{
		Pool:  pool,
		Media: mediaResolver,
		Now:   opts.Now,
	})
	if err != nil {
		return nil, err
	}

	personalityService, err := personality.NewService(personality.ServiceConfig{
		Pool:           pool,
		Cache:          traitCache,
		CacheTTL:       cfg.TraitCacheTTL,
		RetakeInterval: cfg.AssessmentRetakeInterval,
		Now:            opts.Now,
	})
	if err != nil {
		return nil, err
	}

	preferencesService, err := preferences.NewService(pool)
	if err != nil {
		return nil, err
	}

	matchingService, err := matching.NewService(matching.ServiceConfig{
		Pool:         pool,
		Traits:       personalityService,
		Preferences:  preferencesService,
		Kicker:       drainer,
		DefaultLimit: cfg.DiscoverDefaultLimit,
		MaxLimit:     cfg.DiscoverMaxLimit,
		Now:          opts.Now,
	})
	if err != nil {
		return nil, err
	}

	handler, err := router.New(router.Deps{
		Config:      cfg,
		Pool:        pool,
		Tokens:      tokens,
		Auth:        authService,
		Profile:     profileService,
		Personality: personalityService,
		Preferences: preferencesService,
		Matching:    matchingService,
		ProfileRepo: repository.NewProfileRepo(pool),
		Media:       mediaSvc,
		Redis:       opts.Redis,
		Logger:      opts.Logger,
	})
	if err != nil {
		return nil, err
	}

	return &App{
		Config:      cfg,
		Pool:        pool,
		Handler:     handler,
		Auth:        authService,
		Profile:     profileService,
		Personality: personalityService,
		Preferences: preferencesService,
		Matching:    matchingService,
		Tokens:      tokens,
		Drainer:     drainer,
		memoryCache: memoryCache,
	}, nil
}

// StartBackground launches the outbox drainer, the session sweeper and (when the
// in-process cache is in use) its janitor. The returned function blocks until all
// of them have stopped, so shutdown is deterministic.
func (a *App) StartBackground(ctx context.Context) (wait func()) {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := a.Drainer.Run(ctx); err != nil {
			slog.Error("outbox drainer exited", "error", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.runSessionSweeper(ctx)
	}()

	if a.memoryCache != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.memoryCache.StartJanitor(ctx, a.Config.TraitCacheTTL*2)
		}()
	}

	return func() {
		cancel()
		wg.Wait()
	}
}

func (a *App) runSessionSweeper(ctx context.Context) {
	ticker := time.NewTicker(sessionSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := a.Auth.SweepExpiredSessions(ctx)
			if err != nil {
				if ctx.Err() == nil {
					slog.Error("session sweep failed", "error", err)
				}
				continue
			}
			if deleted > 0 {
				slog.Info("expired sessions removed", "count", deleted)
			}
		}
	}
}
