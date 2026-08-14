package notifications

import (
	"log/slog"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/config"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/email"
	"github.com/bits-assignment/dating-platform/backend/internal/push"
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

	svc := NewService(NewStore(pool), redis, mailer, Config{AppBaseURL: cfg.AppBaseURL}, log)
	svc.pusher = newPusher(cfg, log)
	return svc
}

func newPusher(cfg *config.Config, log *slog.Logger) push.Pusher {
	var vendors []push.Pusher
	if cfg.FCMServerKey != "" {
		vendors = append(vendors, push.FCM{ServerKey: cfg.FCMServerKey})
	}
	if cfg.APNSKeyPath != "" && cfg.APNSKeyID != "" && cfg.APNSTeamID != "" {
		pem, err := push.LoadP8(cfg.APNSKeyPath)
		if err != nil {
			if log != nil {
				log.Warn("apns key unreadable", "path", cfg.APNSKeyPath, "error", err)
			}
		} else if len(pem) > 0 {
			vendors = append(vendors, &push.APNs{
				KeyID:      cfg.APNSKeyID,
				TeamID:     cfg.APNSTeamID,
				KeyPEM:     pem,
				BundleID:   cfg.APNSBundleID,
				Production: cfg.APNSProduction,
			})
		}
	}
	if len(vendors) == 0 {
		return push.Nop{}
	}
	return push.Multi{Vendors: vendors}
}
