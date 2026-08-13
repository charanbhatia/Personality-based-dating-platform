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
	}
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
