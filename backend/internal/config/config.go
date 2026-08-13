package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// DevJWTSecret is the placeholder secret shipped in env.example. Booting with it
// outside development is refused so a deployment cannot accidentally sign tokens
// with a public key.
const DevJWTSecret = "dev-secret-change-in-production"

// MinJWTSecretLen is the shortest secret accepted outside development. HS256
// keys shorter than the 256-bit digest weaken the MAC.
const MinJWTSecretLen = 32

type Config struct {
	Env         string
	Port        string
	DatabaseURL string

	// Auth
	JWTSecret        string
	JWTIssuer        string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	PasswordResetTTL time.Duration

	// Personality
	AssessmentRetakeInterval time.Duration

	// Discovery
	DiscoverDefaultLimit int
	DiscoverMaxLimit     int
	TraitCacheTTL        time.Duration

	// Outbox (drained by the shared worker; see roadmap §7)
	OutboxBatchSize    int
	OutboxPollInterval time.Duration
	OutboxMaxAttempts  int

	// Server
	CORSAllowedOrigins []string
	ReadHeaderTimeout  time.Duration
	ShutdownTimeout    time.Duration
	DBMaxConns         int32
}

func (c *Config) IsProduction() bool {
	return c.Env == "production" || c.Env == "prod" || c.Env == "staging"
}

// Load reads configuration from the environment, applying defaults and
// rejecting combinations that would be unsafe or self-contradictory at runtime.
func Load() (*Config, error) {
	c := &Config{
		Env:         strings.ToLower(getEnv("APP_ENV", "development")),
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://localhost:5432/dating_platform?sslmode=disable"),

		JWTSecret: getEnv("JWT_SECRET", DevJWTSecret),
		JWTIssuer: getEnv("JWT_ISSUER", "dating-platform"),
	}

	var errs []error
	collect := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	var err error
	c.AccessTokenTTL, err = getDuration("ACCESS_TOKEN_TTL", 15*time.Minute)
	collect(err)
	c.RefreshTokenTTL, err = getDuration("REFRESH_TOKEN_TTL", 30*24*time.Hour)
	collect(err)
	c.PasswordResetTTL, err = getDuration("PASSWORD_RESET_TTL", time.Hour)
	collect(err)
	c.AssessmentRetakeInterval, err = getDuration("ASSESSMENT_RETAKE_INTERVAL", 30*24*time.Hour)
	collect(err)
	c.TraitCacheTTL, err = getDuration("TRAIT_CACHE_TTL", time.Minute)
	collect(err)
	c.OutboxPollInterval, err = getDuration("OUTBOX_POLL_INTERVAL", 2*time.Second)
	collect(err)
	c.ReadHeaderTimeout, err = getDuration("READ_HEADER_TIMEOUT", 10*time.Second)
	collect(err)
	c.ShutdownTimeout, err = getDuration("SHUTDOWN_TIMEOUT", 15*time.Second)
	collect(err)

	c.DiscoverDefaultLimit, err = getInt("DISCOVER_DEFAULT_LIMIT", 20)
	collect(err)
	c.DiscoverMaxLimit, err = getInt("DISCOVER_MAX_LIMIT", 50)
	collect(err)
	c.OutboxBatchSize, err = getInt("OUTBOX_BATCH_SIZE", 100)
	collect(err)
	c.OutboxMaxAttempts, err = getInt("OUTBOX_MAX_ATTEMPTS", 10)
	collect(err)
	maxConns, err := getInt("DB_MAX_CONNS", 0)
	collect(err)
	c.DBMaxConns = int32(maxConns)

	c.CORSAllowedOrigins = splitAndTrim(getEnv("CORS_ALLOWED_ORIGINS", ""))

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	var errs []error

	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.JWTSecret == "" {
		errs = append(errs, errors.New("JWT_SECRET is required"))
	}
	if c.IsProduction() {
		if c.JWTSecret == DevJWTSecret {
			errs = append(errs, fmt.Errorf("JWT_SECRET must not be the development default when APP_ENV=%s", c.Env))
		}
		if len(c.JWTSecret) < MinJWTSecretLen {
			errs = append(errs, fmt.Errorf("JWT_SECRET must be at least %d characters when APP_ENV=%s", MinJWTSecretLen, c.Env))
		}
		if len(c.CORSAllowedOrigins) == 0 {
			errs = append(errs, fmt.Errorf("CORS_ALLOWED_ORIGINS must be set when APP_ENV=%s", c.Env))
		}
	}
	if c.AccessTokenTTL <= 0 {
		errs = append(errs, errors.New("ACCESS_TOKEN_TTL must be positive"))
	}
	if c.RefreshTokenTTL <= c.AccessTokenTTL {
		errs = append(errs, errors.New("REFRESH_TOKEN_TTL must be longer than ACCESS_TOKEN_TTL"))
	}
	if c.PasswordResetTTL <= 0 {
		errs = append(errs, errors.New("PASSWORD_RESET_TTL must be positive"))
	}
	if c.AssessmentRetakeInterval < 0 {
		errs = append(errs, errors.New("ASSESSMENT_RETAKE_INTERVAL must not be negative"))
	}
	if c.DiscoverDefaultLimit < 1 {
		errs = append(errs, errors.New("DISCOVER_DEFAULT_LIMIT must be at least 1"))
	}
	if c.DiscoverMaxLimit < c.DiscoverDefaultLimit {
		errs = append(errs, errors.New("DISCOVER_MAX_LIMIT must be at least DISCOVER_DEFAULT_LIMIT"))
	}
	if c.OutboxBatchSize < 1 {
		errs = append(errs, errors.New("OUTBOX_BATCH_SIZE must be at least 1"))
	}
	if c.OutboxMaxAttempts < 1 {
		errs = append(errs, errors.New("OUTBOX_MAX_ATTEMPTS must be at least 1"))
	}
	if c.OutboxPollInterval <= 0 {
		errs = append(errs, errors.New("OUTBOX_POLL_INTERVAL must be positive"))
	}
	if c.DBMaxConns < 0 {
		errs = append(errs, errors.New("DB_MAX_CONNS must not be negative"))
	}
	if _, err := strconv.Atoi(c.Port); err != nil {
		errs = append(errs, fmt.Errorf("PORT must be numeric, got %q", c.Port))
	}
	return errors.Join(errs...)
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration such as 15m or 720h, got %q", key, raw)
	}
	return d, nil
}

func getInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", key, raw)
	}
	return n, nil
}

func splitAndTrim(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
