package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env         string
	Port        string
	DatabaseURL string
	JWTSecret   string

	RedisURL string

	LogLevel    string
	CORSOrigins []string

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

func Load() *Config {
	return &Config{
		Env:         getEnv("ENV", "development"),
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://localhost:5432/dating_platform?sslmode=disable"),
		JWTSecret:   getEnv("JWT_SECRET", "dev-secret-change-in-production"),

		RedisURL: getEnv("REDIS_URL", ""),

		LogLevel:    getEnv("LOG_LEVEL", "info"),
		CORSOrigins: getCSV("CORS_ORIGINS", "http://localhost:5173,http://localhost:3000"),

		RateLimitEnabled: getBool("RATE_LIMIT_ENABLED", true),
		RateLimitWindow:  time.Duration(getInt("RATE_LIMIT_WINDOW_SECONDS", 60)) * time.Second,
		RateLimitGlobal:  getInt("RATE_LIMIT_GLOBAL", 600),
		RateLimitAuth:    getInt("RATE_LIMIT_AUTH", 20),

		MessageSendLimit:  getInt("RATE_LIMIT_MESSAGE_SEND", 5),
		MessageSendWindow: time.Duration(getInt("RATE_LIMIT_MESSAGE_WINDOW_SECONDS", 1)) * time.Second,
		MaxMessageLength:  getInt("MAX_MESSAGE_LENGTH", 4000),
		MatchGateEnabled:  getBool("MATCH_GATE_ENABLED", true),

		QueueMaxLen:       int64(getInt("QUEUE_MAX_LEN", 10000)),
		QueueMaxAttempts:  getInt("QUEUE_MAX_ATTEMPTS", 5),
		QueueRetryDelay:   time.Duration(getInt("QUEUE_RETRY_DELAY_SECONDS", 30)) * time.Second,
		QueueBlockTimeout: time.Duration(getInt("QUEUE_BLOCK_SECONDS", 5)) * time.Second,
		WorkerConcurrency: getInt("WORKER_CONCURRENCY", 4),
		WorkerName:        getEnv("WORKER_NAME", defaultWorkerName()),
		WorkerMetricsPort: getEnv("WORKER_METRICS_PORT", "9091"),

		S3Endpoint:      getEnv("S3_ENDPOINT", ""),
		S3Region:        getEnv("S3_REGION", "us-east-1"),
		S3Bucket:        getEnv("S3_BUCKET", "dating-media"),
		S3AccessKey:     getEnv("S3_ACCESS_KEY", ""),
		S3SecretKey:     getEnv("S3_SECRET_KEY", ""),
		S3PublicBaseURL: getEnv("S3_PUBLIC_BASE_URL", ""),
		S3PathStyle:     getBool("S3_PATH_STYLE", true),
		S3PresignTTL:    time.Duration(getInt("S3_PRESIGN_TTL_SECONDS", 900)) * time.Second,

		MediaMaxBytes:     int64(getInt("MEDIA_MAX_BYTES", 10<<20)),
		MediaAllowedTypes: getCSV("MEDIA_ALLOWED_TYPES", "image/jpeg,image/png,image/webp"),
		MediaDailyLimit:   getInt("MEDIA_DAILY_UPLOAD_LIMIT", 50),
		ThumbnailMaxEdge:  getInt("THUMBNAIL_MAX_EDGE", 512),

		WSAllowedOrigins:  getCSV("WS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:3000"),
		WSMaxMessageBytes: int64(getInt("WS_MAX_MESSAGE_BYTES", 16<<10)),
		WSPingInterval:    time.Duration(getInt("WS_PING_INTERVAL_SECONDS", 30)) * time.Second,
		WSPongTimeout:     time.Duration(getInt("WS_PONG_TIMEOUT_SECONDS", 70)) * time.Second,
		WSWriteTimeout:    time.Duration(getInt("WS_WRITE_TIMEOUT_SECONDS", 10)) * time.Second,

		MetricsEnabled:        getBool("METRICS_ENABLED", true),
		MetricsSampleInterval: time.Duration(getInt("METRICS_SAMPLE_INTERVAL_SECONDS", 15)) * time.Second,

		AppBaseURL:   getEnv("APP_BASE_URL", "http://localhost:5173"),
		EmailMode:    getEnv("EMAIL_MODE", "log"),
		EmailFrom:    getEnv("EMAIL_FROM", "no-reply@kindred.local"),
		SMTPHost:     getEnv("SMTP_HOST", ""),
		SMTPPort:     getInt("SMTP_PORT", 1025),
		SMTPUsername: getEnv("SMTP_USERNAME", ""),
		SMTPPassword: getEnv("SMTP_PASSWORD", ""),
	}
}

// MediaEnabled reports whether object storage is configured. Media endpoints
// answer 503 when it is not, leaving the rest of the API usable.
func (c *Config) MediaEnabled() bool {
	return c.S3Endpoint != "" && c.S3AccessKey != "" && c.S3SecretKey != ""
}

// defaultWorkerName keeps consumer names distinct per process so Redis can tell
// replicas apart when reclaiming pending events.
func defaultWorkerName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "worker"
	}
	return host
}

// RedisEnabled reports whether Redis-backed features (rate limiting, pub/sub,
// queues) should be wired. The API stays fully functional without Redis so the
// stack can run with Postgres alone.
func (c *Config) RedisEnabled() bool { return c.RedisURL != "" }

func (c *Config) IsProduction() bool { return c.Env == "production" }

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getCSV(key, fallback string) []string {
	raw := getEnv(key, fallback)
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
