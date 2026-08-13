package notifications

import (
	"log/slog"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/config"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/email"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

// NewServiceFromConfig builds the notifications service shared by the API and
// the worker.
func NewServiceFromConfig(pool *pgxpool.Pool, redis *goredis.Client, cfg *config.Config, log *slog.Logger) *Service {
	mailer := email.New(email.Config{
		Mode:     cfg.EmailMode,
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
		From:     cfg.EmailFrom,
	}, log)

	return NewService(NewStore(pool), redis, mailer, Config{AppBaseURL: cfg.AppBaseURL}, log)
}
