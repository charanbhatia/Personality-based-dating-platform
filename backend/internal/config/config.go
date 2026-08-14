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
	LogLevel    string
	LogFormat   string

	// Auth
	JWTSecret            string
	JWTIssuer            string
	AccessTokenTTL       time.Duration
	RefreshTokenTTL      time.Duration
	PasswordResetTTL     time.Duration
	EmailVerificationTTL time.Duration

	// Personality
	AssessmentRetakeInterval time.Duration

	// Discovery
	DiscoverDefaultLimit int
	DiscoverMaxLimit     int
	TraitCacheTTL        time.Duration

	// Outbox
	OutboxBatchSize    int
	OutboxPollInterval time.Duration
	OutboxMaxAttempts  int

	// Server
	CORSAllowedOrigins []string
	CORSOrigins        []string
	ReadHeaderTimeout  time.Duration
	ShutdownTimeout    time.Duration
	DBMaxConns         int32
	DBMinConns         int32

	// Redis / platform (Person C)
	RedisURL string

	RateLimitEnabled bool
	RateLimitWindow  time.Duration
	RateLimitGlobal  int
	RateLimitAuth    int

	MessageSendLimit  int
	MessageSendWindow time.Duration
	MaxMessageLength  int
	MatchGateEnabled  bool

	QueueMaxLen       int64
	QueueMaxAttempts  int
	QueueRetryDelay   time.Duration
	QueueBlockTimeout time.Duration
	WorkerConcurrency int
	WorkerName        string
	WorkerMetricsPort string

	S3Endpoint      string
	S3Region        string
	S3Bucket        string
	S3AccessKey     string
	S3SecretKey     string
	S3PublicBaseURL string
	S3PathStyle     bool
	S3PresignTTL    time.Duration

	MediaMaxBytes     int64
	MediaAllowedTypes []string
	MediaDailyLimit   int
	ThumbnailMaxEdge  int

	WSAllowedOrigins  []string
	WSMaxMessageBytes int64
	WSPingInterval    time.Duration
	WSPongTimeout     time.Duration
	WSWriteTimeout    time.Duration

	MetricsEnabled        bool
	MetricsSampleInterval time.Duration

	AppBaseURL   string
	EmailMode    string
	EmailFrom    string
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
}

func (c *Config) IsProduction() bool {
	return c.Env == "production" || c.Env == "prod" || c.Env == "staging"
}

func (c *Config) RedisEnabled() bool { return c.RedisURL != "" }

func (c *Config) MediaEnabled() bool {
	return c.S3Endpoint != "" && c.S3AccessKey != "" && c.S3SecretKey != ""
}

// Load reads configuration from the environment, applying defaults and
// rejecting combinations that would be unsafe or self-contradictory at runtime.
func Load() (*Config, error) {
	env := strings.ToLower(getEnv("APP_ENV", ""))
	if env == "" {
		env = strings.ToLower(getEnv("ENV", "development"))
	}

	c := &Config{
		Env:         env,
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://localhost:5432/dating_platform?sslmode=disable"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		LogFormat:   getEnv("LOG_FORMAT", "text"),

		JWTSecret: getEnv("JWT_SECRET", DevJWTSecret),
		JWTIssuer: getEnv("JWT_ISSUER", "dating-platform"),

		RedisURL: getEnv("REDIS_URL", ""),

		S3Endpoint:      getEnv("S3_ENDPOINT", ""),
		S3Region:        getEnv("S3_REGION", "us-east-1"),
		S3Bucket:        getEnv("S3_BUCKET", "dating-media"),
		S3AccessKey:     getEnv("S3_ACCESS_KEY", ""),
		S3SecretKey:     getEnv("S3_SECRET_KEY", ""),
		S3PublicBaseURL: getEnv("S3_PUBLIC_BASE_URL", ""),
		S3PathStyle:     getBool("S3_PATH_STYLE", true),

		WorkerName:        getEnv("WORKER_NAME", defaultWorkerName()),
		WorkerMetricsPort: getEnv("WORKER_METRICS_PORT", "9091"),

		AppBaseURL:   getEnv("APP_BASE_URL", "http://localhost:5173"),
		EmailMode:    getEnv("EMAIL_MODE", "log"),
		EmailFrom:    getEnv("EMAIL_FROM", "no-reply@kindred.local"),
		SMTPHost:     getEnv("SMTP_HOST", ""),
		SMTPUsername: getEnv("SMTP_USERNAME", ""),
		SMTPPassword: getEnv("SMTP_PASSWORD", ""),
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
	c.EmailVerificationTTL, err = getDuration("EMAIL_VERIFICATION_TTL", 24*time.Hour)
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
	minConns, err := getInt("DB_MIN_CONNS", 0)
	collect(err)
	c.DBMinConns = int32(minConns)

	c.RateLimitEnabled = getBool("RATE_LIMIT_ENABLED", true)
	c.RateLimitWindow = time.Duration(getIntDefault("RATE_LIMIT_WINDOW_SECONDS", 60)) * time.Second
	c.RateLimitGlobal = getIntDefault("RATE_LIMIT_GLOBAL", 600)
	c.RateLimitAuth = getIntDefault("RATE_LIMIT_AUTH", 20)

	c.MessageSendLimit = getIntDefault("RATE_LIMIT_MESSAGE_SEND", 5)
	c.MessageSendWindow = time.Duration(getIntDefault("RATE_LIMIT_MESSAGE_WINDOW_SECONDS", 1)) * time.Second
	c.MaxMessageLength = getIntDefault("MAX_MESSAGE_LENGTH", 4000)
	c.MatchGateEnabled = getBool("MATCH_GATE_ENABLED", true)

	c.QueueMaxLen = int64(getIntDefault("QUEUE_MAX_LEN", 10000))
	c.QueueMaxAttempts = getIntDefault("QUEUE_MAX_ATTEMPTS", 5)
	c.QueueRetryDelay = time.Duration(getIntDefault("QUEUE_RETRY_DELAY_SECONDS", 30)) * time.Second
	c.QueueBlockTimeout = time.Duration(getIntDefault("QUEUE_BLOCK_SECONDS", 5)) * time.Second
	c.WorkerConcurrency = getIntDefault("WORKER_CONCURRENCY", 4)

	c.S3PresignTTL = time.Duration(getIntDefault("S3_PRESIGN_TTL_SECONDS", 900)) * time.Second
	c.MediaMaxBytes = int64(getIntDefault("MEDIA_MAX_BYTES", 10<<20))
	c.MediaAllowedTypes = getCSV("MEDIA_ALLOWED_TYPES", "image/jpeg,image/png,image/webp")
	c.MediaDailyLimit = getIntDefault("MEDIA_DAILY_UPLOAD_LIMIT", 50)
	c.ThumbnailMaxEdge = getIntDefault("THUMBNAIL_MAX_EDGE", 512)

	c.WSAllowedOrigins = getCSV("WS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:3000")
	c.WSMaxMessageBytes = int64(getIntDefault("WS_MAX_MESSAGE_BYTES", 16<<10))
	c.WSPingInterval = time.Duration(getIntDefault("WS_PING_INTERVAL_SECONDS", 30)) * time.Second
	c.WSPongTimeout = time.Duration(getIntDefault("WS_PONG_TIMEOUT_SECONDS", 70)) * time.Second
	c.WSWriteTimeout = time.Duration(getIntDefault("WS_WRITE_TIMEOUT_SECONDS", 10)) * time.Second

	c.MetricsEnabled = getBool("METRICS_ENABLED", true)
	c.MetricsSampleInterval = time.Duration(getIntDefault("METRICS_SAMPLE_INTERVAL_SECONDS", 15)) * time.Second

	c.SMTPPort = getIntDefault("SMTP_PORT", 1025)

	origins := splitAndTrim(getEnv("CORS_ALLOWED_ORIGINS", ""))
	if len(origins) == 0 {
		origins = splitAndTrim(getEnv("CORS_ORIGINS", ""))
	}
	c.CORSAllowedOrigins = origins
	c.CORSOrigins = origins

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
		if len(c.CORSAllowedOrigins) == 0 && len(c.CORSOrigins) == 0 {
			errs = append(errs, fmt.Errorf("CORS_ALLOWED_ORIGINS must be set when APP_ENV=%s", c.Env))
		}
		for _, origin := range append(append([]string{}, c.CORSAllowedOrigins...), c.CORSOrigins...) {
			if origin == "*" {
				errs = append(errs, fmt.Errorf("CORS_ALLOWED_ORIGINS must not include * when APP_ENV=%s", c.Env))
				break
			}
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
	if c.EmailVerificationTTL <= 0 {
		errs = append(errs, errors.New("EMAIL_VERIFICATION_TTL must be positive"))
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

func getIntDefault(key string, fallback int) int {
	n, _ := getInt(key, fallback)
	return n
}

func getBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return b
}

func getCSV(key, fallback string) []string {
	raw := getEnv(key, fallback)
	return splitAndTrim(raw)
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

func defaultWorkerName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "worker"
	}
	return host
}
