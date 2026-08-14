package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// FCM sends via the legacy FCM HTTP API when a server key is configured.
type FCM struct {
	ServerKey string
	Client    *http.Client
}

func (f FCM) Push(ctx context.Context, token Token, note Note) error {
	if f.ServerKey == "" {
		return nil
	}
	if token.Platform == "ios" {
		return nil
	}
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	body, err := json.Marshal(map[string]any{
		"to": token.Token,
		"notification": map[string]string{
			"title": note.Title,
			"body":  note.Body,
		},
		"data": note.Data,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://fcm.googleapis.com/fcm/send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "key="+strings.TrimSpace(f.ServerKey))
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("fcm: HTTP %d", res.StatusCode)
	}
	return nil
}
