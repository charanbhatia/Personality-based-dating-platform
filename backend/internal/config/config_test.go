package config

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"
)

// setEnv clears every variable Load reads before applying the given ones, so a
// test never inherits the developer's shell or another test's leftovers.
func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	known := []string{
		"APP_ENV", "PORT", "DATABASE_URL", "JWT_SECRET", "JWT_ISSUER",
		"ACCESS_TOKEN_TTL", "REFRESH_TOKEN_TTL", "PASSWORD_RESET_TTL",
		"ASSESSMENT_RETAKE_INTERVAL", "DISCOVER_DEFAULT_LIMIT", "DISCOVER_MAX_LIMIT",
		"TRAIT_CACHE_TTL", "OUTBOX_BATCH_SIZE", "OUTBOX_POLL_INTERVAL",
		"OUTBOX_MAX_ATTEMPTS", "CORS_ALLOWED_ORIGINS", "READ_HEADER_TIMEOUT",
		"SHUTDOWN_TIMEOUT", "DB_MAX_CONNS",
	}
	for _, key := range known {
		t.Setenv(key, "")
	}
	for key, value := range vars {
		t.Setenv(key, value)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, nil)

	c, err := Load()
	if err != nil {
		t.Fatalf("Load with an empty environment: %v", err)
	}

	// An empty environment must produce a working development server, or a fresh
	// clone cannot be run with one command.
	if c.Env != "development" || c.IsProduction() {
		t.Errorf("Env = %q, IsProduction = %v, want development", c.Env, c.IsProduction())
	}
	if c.Port != "8080" {
		t.Errorf("Port = %q, want 8080", c.Port)
	}
	if c.JWTSecret != DevJWTSecret {
		t.Errorf("JWTSecret = %q, want the development placeholder", c.JWTSecret)
	}
	if c.AccessTokenTTL != 15*time.Minute {
		t.Errorf("AccessTokenTTL = %v, want 15m", c.AccessTokenTTL)
	}
	if c.RefreshTokenTTL != 30*24*time.Hour {
		t.Errorf("RefreshTokenTTL = %v, want 720h", c.RefreshTokenTTL)
	}
	if c.DiscoverDefaultLimit != 20 || c.DiscoverMaxLimit != 50 {
		t.Errorf("discover limits = %d/%d, want 20/50", c.DiscoverDefaultLimit, c.DiscoverMaxLimit)
	}
	if len(c.CORSAllowedOrigins) != 0 {
		t.Errorf("CORSAllowedOrigins = %v, want empty so the origin is reflected locally", c.CORSAllowedOrigins)
	}
}

func TestLoadParsesOverrides(t *testing.T) {
	setEnv(t, map[string]string{
		"APP_ENV":                    "PRODUCTION",
		"PORT":                       "9000",
		"DATABASE_URL":               "postgres://db/app",
		"JWT_SECRET":                 strings.Repeat("s", MinJWTSecretLen),
		"JWT_ISSUER":                 "custom-issuer",
		"ACCESS_TOKEN_TTL":           "5m",
		"REFRESH_TOKEN_TTL":          "48h",
		"PASSWORD_RESET_TTL":         "30m",
		"ASSESSMENT_RETAKE_INTERVAL": "0",
		"DISCOVER_DEFAULT_LIMIT":     "10",
		"DISCOVER_MAX_LIMIT":         "25",
		"OUTBOX_BATCH_SIZE":          "50",
		"OUTBOX_POLL_INTERVAL":       "500ms",
		"OUTBOX_MAX_ATTEMPTS":        "3",
		"DB_MAX_CONNS":               "12",
		// Deliberately padded and with a trailing separator, which is what an
		// operator editing a long line tends to leave behind.
		"CORS_ALLOWED_ORIGINS": " https://app.example.test , https://admin.example.test ,",
	})

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !c.IsProduction() {
		t.Error("APP_ENV=PRODUCTION was not recognised; case must not matter")
	}
	if c.JWTIssuer != "custom-issuer" || c.Port != "9000" {
		t.Errorf("issuer/port = %q/%q, want custom-issuer/9000", c.JWTIssuer, c.Port)
	}
	if c.AccessTokenTTL != 5*time.Minute || c.RefreshTokenTTL != 48*time.Hour {
		t.Errorf("TTLs = %v/%v, want 5m/48h", c.AccessTokenTTL, c.RefreshTokenTTL)
	}
	// Zero is meaningful here: it turns off the retake restriction for demos.
	if c.AssessmentRetakeInterval != 0 {
		t.Errorf("AssessmentRetakeInterval = %v, want 0", c.AssessmentRetakeInterval)
	}
	if c.OutboxPollInterval != 500*time.Millisecond {
		t.Errorf("OutboxPollInterval = %v, want 500ms", c.OutboxPollInterval)
	}
	if c.DBMaxConns != 12 {
		t.Errorf("DBMaxConns = %d, want 12", c.DBMaxConns)
	}
	want := []string{"https://app.example.test", "https://admin.example.test"}
	if len(c.CORSAllowedOrigins) != len(want) {
		t.Fatalf("CORSAllowedOrigins = %v, want %v", c.CORSAllowedOrigins, want)
	}
	for i := range want {
		if c.CORSAllowedOrigins[i] != want[i] {
			t.Fatalf("CORSAllowedOrigins = %v, want %v", c.CORSAllowedOrigins, want)
		}
	}
}

func TestLoadRejectsUnsafeProduction(t *testing.T) {
	base := map[string]string{
		"APP_ENV":              "production",
		"JWT_SECRET":           strings.Repeat("s", MinJWTSecretLen),
		"CORS_ALLOWED_ORIGINS": "https://app.example.test",
	}
	with := func(overrides map[string]string) map[string]string {
		out := make(map[string]string, len(base)+len(overrides))
		for k, v := range base {
			out[k] = v
		}
		for k, v := range overrides {
			out[k] = v
		}
		return out
	}

	cases := map[string]struct {
		vars []string
		env  map[string]string
	}{
		"development secret": {
			[]string{"JWT_SECRET"}, with(map[string]string{"JWT_SECRET": DevJWTSecret})},
		"short secret": {
			[]string{"JWT_SECRET"}, with(map[string]string{"JWT_SECRET": strings.Repeat("s", MinJWTSecretLen-1)})},
		"reflected CORS": {
			[]string{"CORS_ALLOWED_ORIGINS"}, with(map[string]string{"CORS_ALLOWED_ORIGINS": ""})},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			setEnv(t, tc.env)
			_, err := Load()
			if err == nil {
				t.Fatal("Load succeeded, want a refusal")
			}
			// The message must name the variable, or an operator cannot act on it.
			for _, v := range tc.vars {
				if !strings.Contains(err.Error(), v) {
					t.Errorf("error %q does not mention %s", err, v)
				}
			}
		})
	}

	// The same values are fine outside production.
	setEnv(t, map[string]string{"APP_ENV": "development", "JWT_SECRET": DevJWTSecret})
	if _, err := Load(); err != nil {
		t.Errorf("development rejected the placeholder secret: %v", err)
	}
}

func TestLoadRejectsContradictoryValues(t *testing.T) {
	cases := map[string]map[string]string{
		"refresh shorter than access": {"ACCESS_TOKEN_TTL": "1h", "REFRESH_TOKEN_TTL": "10m"},
		"refresh equal to access":     {"ACCESS_TOKEN_TTL": "1h", "REFRESH_TOKEN_TTL": "1h"},
		"zero access ttl":             {"ACCESS_TOKEN_TTL": "0"},
		"negative retake":             {"ASSESSMENT_RETAKE_INTERVAL": "-1h"},
		"max below default":           {"DISCOVER_DEFAULT_LIMIT": "50", "DISCOVER_MAX_LIMIT": "10"},
		"zero default limit":          {"DISCOVER_DEFAULT_LIMIT": "0"},
		"zero outbox batch":           {"OUTBOX_BATCH_SIZE": "0"},
		"zero outbox attempts":        {"OUTBOX_MAX_ATTEMPTS": "0"},
		"zero poll interval":          {"OUTBOX_POLL_INTERVAL": "0"},
		"negative connections":        {"DB_MAX_CONNS": "-1"},
		"non-numeric port":            {"PORT": "http"},
		"unparseable duration":        {"ACCESS_TOKEN_TTL": "fifteen minutes"},
		"unparseable integer":         {"DISCOVER_MAX_LIMIT": "many"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			setEnv(t, env)
			if _, err := Load(); err == nil {
				t.Fatal("Load succeeded, want a refusal")
			}
		})
	}
}

func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	setEnv(t, map[string]string{"PORT": "http", "OUTBOX_BATCH_SIZE": "0", "ACCESS_TOKEN_TTL": "0"})

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded, want a refusal")
	}
	// Fixing one variable per restart is a miserable loop; all of them are
	// reported together.
	for _, want := range []string{"PORT", "OUTBOX_BATCH_SIZE", "ACCESS_TOKEN_TTL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

// TestEnvExampleIsSafeToCopy keeps the shipped sample honest. Its placeholder
// secret has to be the exact value the production guard blocks, otherwise
// copying the file into a deployment would sign tokens with a public key and
// pass validation.
func TestEnvExampleIsSafeToCopy(t *testing.T) {
	vars := readEnvFile(t, "../../env.example")

	if got := vars["JWT_SECRET"]; got != DevJWTSecret {
		t.Errorf("env.example JWT_SECRET = %q, want %q so the production guard catches it", got, DevJWTSecret)
	}

	// Every documented value must also survive parsing, so following the sample
	// cannot produce a server that refuses to boot.
	setEnv(t, vars)
	if _, err := Load(); err != nil {
		t.Fatalf("env.example does not load: %v", err)
	}

	setEnv(t, mergeEnv(vars, map[string]string{"APP_ENV": "production"}))
	if _, err := Load(); err == nil {
		t.Error("env.example loads as production, want the placeholder secret refused")
	}
}

func mergeEnv(base, overrides map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}

func readEnvFile(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	vars := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			t.Errorf("%s has a line that is neither a comment nor an assignment: %q", path, line)
			continue
		}
		vars[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return vars
}
