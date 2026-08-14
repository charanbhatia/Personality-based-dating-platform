package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// APNs sends via HTTP/2 to Apple's push service when a .p8 key is configured.
type APNs struct {
	KeyID      string
	TeamID     string
	KeyPEM     []byte
	BundleID   string
	Production bool
	Client     *http.Client

	mu        sync.Mutex
	jwt       string
	jwtExpiry time.Time
	key       *ecdsa.PrivateKey
}

func (a *APNs) Push(ctx context.Context, token Token, note Note) error {
	if len(a.KeyPEM) == 0 || a.KeyID == "" || a.TeamID == "" || a.BundleID == "" {
		return nil
	}
	if token.Platform != "ios" {
		return nil
	}
	auth, err := a.bearer()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"aps": map[string]any{
			"alert": map[string]string{"title": note.Title, "body": note.Body},
			"sound": "default",
		},
		"data": note.Data,
	})
	if err != nil {
		return err
	}
	host := "https://api.sandbox.push.apple.com"
	if a.Production {
		host = "https://api.push.apple.com"
	}
	url := host + "/3/device/" + token.Token
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "bearer "+auth)
	req.Header.Set("apns-topic", a.BundleID)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("apns: HTTP %d", res.StatusCode)
	}
	return nil
}

func (a *APNs) bearer() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jwt != "" && time.Now().Before(a.jwtExpiry) {
		return a.jwt, nil
	}
	key, err := a.privateKey()
	if err != nil {
		return "", err
	}
	now := time.Now().Unix()
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": a.KeyID})
	claims, _ := json.Marshal(map[string]any{"iss": a.TeamID, "iat": now})
	signing := b64(header) + "." + b64(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	sig := append(pad32(r.Bytes()), pad32(s.Bytes())...)
	token := signing + "." + b64(sig)
	a.jwt = token
	a.jwtExpiry = time.Now().Add(40 * time.Minute)
	return token, nil
}

func (a *APNs) privateKey() (*ecdsa.PrivateKey, error) {
	if a.key != nil {
		return a.key, nil
	}
	block, _ := pem.Decode(a.KeyPEM)
	if block == nil {
		return nil, fmt.Errorf("apns: invalid p8 pem")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("apns: expected P-256 private key")
	}
	a.key = key
	return key, nil
}

func b64(raw []byte) string {
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(raw), "=")
}

func pad32(b []byte) []byte {
	if len(b) >= 32 {
		return b[len(b)-32:]
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

// LoadP8 reads an APNs .p8 file. Empty path is a no-op.
func LoadP8(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	return os.ReadFile(path)
}
