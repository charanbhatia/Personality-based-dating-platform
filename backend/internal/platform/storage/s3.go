// Package storage is a minimal S3-compatible client covering the object
// operations this service needs: presigned uploads for browsers, plus server
// side get/put/delete. It signs requests with AWS Signature Version 4 and has
// no external dependencies, which keeps the module on its declared Go version.
package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	algorithm       = "AWS4-HMAC-SHA256"
	service         = "s3"
	unsignedPayload = "UNSIGNED-PAYLOAD"
	amzDateFormat   = "20060102T150405Z"
	dateFormat      = "20060102"
	maxObjectRead   = 32 << 20
)

type Config struct {
	Endpoint      string
	Region        string
	Bucket        string
	AccessKey     string
	SecretKey     string
	PublicBaseURL string
	// PathStyle puts the bucket in the URL path rather than the host, which is
	// what MinIO and most S3-compatible servers expect.
	PathStyle  bool
	PresignTTL time.Duration
}

type Client struct {
	cfg    Config
	scheme string
	host   string
	http   *http.Client
}

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("endpoint must include scheme and host, got %q", cfg.Endpoint)
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.PresignTTL <= 0 {
		cfg.PresignTTL = 15 * time.Minute
	}

	return &Client{
		cfg:    cfg,
		scheme: u.Scheme,
		host:   u.Host,
		http:   &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (c *Client) Bucket() string { return c.cfg.Bucket }

// PublicURL is the address a browser uses to read an object. It is only
// reachable when the bucket or prefix allows anonymous reads.
func (c *Client) PublicURL(key string) string {
	base := c.cfg.PublicBaseURL
	if base == "" {
		return c.objectURL(key)
	}
	return strings.TrimRight(base, "/") + "/" + c.cfg.Bucket + "/" + key
}

// PresignPut returns an upload URL. contentType is signed, so the client must
// send exactly that Content-Type header.
func (c *Client) PresignPut(key, contentType string, ttl time.Duration) (string, error) {
	return c.presign(http.MethodPut, key, contentType, ttl)
}

func (c *Client) PresignGet(key string, ttl time.Duration) (string, error) {
	return c.presign(http.MethodGet, key, "", ttl)
}

func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	signed, err := c.presign(http.MethodGet, key, "", c.cfg.PresignTTL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signed, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, responseError("get", key, resp)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxObjectRead))
}

func (c *Client) Put(ctx context.Context, key, contentType string, body []byte) error {
	signed, err := c.presign(http.MethodPut, key, contentType, c.cfg.PresignTTL)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, signed, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = int64(len(body))

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return responseError("put", key, resp)
	}
	return nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	signed, err := c.presign(http.MethodDelete, key, "", c.cfg.PresignTTL)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, signed, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return responseError("delete", key, resp)
	}
	return nil
}

// Exists reports whether an object is present, used to confirm a client upload
// actually landed before queueing work for it.
func (c *Client) Exists(ctx context.Context, key string) (bool, int64, error) {
	// The method is part of the signature, so a HEAD request needs a URL
	// presigned for HEAD.
	signed, err := c.presign(http.MethodHead, key, "", c.cfg.PresignTTL)
	if err != nil {
		return false, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, signed, nil)
	if err != nil {
		return false, 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, resp.ContentLength, nil
	case http.StatusNotFound, http.StatusForbidden:
		return false, 0, nil
	default:
		return false, 0, responseError("head", key, resp)
	}
}

func (c *Client) objectURL(key string) string {
	if c.cfg.PathStyle {
		return fmt.Sprintf("%s://%s/%s/%s", c.scheme, c.host, c.cfg.Bucket, key)
	}
	return fmt.Sprintf("%s://%s.%s/%s", c.scheme, c.cfg.Bucket, c.host, key)
}

func (c *Client) canonicalURI(key string) string {
	encoded := encodePath(key)
	if c.cfg.PathStyle {
		return "/" + c.cfg.Bucket + "/" + encoded
	}
	return "/" + encoded
}

func (c *Client) requestHost() string {
	if c.cfg.PathStyle {
		return c.host
	}
	return c.cfg.Bucket + "." + c.host
}

// presign builds a query-string signed URL per AWS Signature Version 4.
func (c *Client) presign(method, key, contentType string, ttl time.Duration) (string, error) {
	if c.cfg.AccessKey == "" || c.cfg.SecretKey == "" {
		return "", fmt.Errorf("storage credentials are not configured")
	}
	if ttl <= 0 {
		ttl = c.cfg.PresignTTL
	}

	now := time.Now().UTC()
	amzDate := now.Format(amzDateFormat)
	scope := strings.Join([]string{now.Format(dateFormat), c.cfg.Region, service, "aws4_request"}, "/")

	signedHeaders := "host"
	headers := map[string]string{"host": c.requestHost()}
	if contentType != "" {
		signedHeaders = "content-type;host"
		headers["content-type"] = contentType
	}

	query := map[string]string{
		"X-Amz-Algorithm":     algorithm,
		"X-Amz-Credential":    c.cfg.AccessKey + "/" + scope,
		"X-Amz-Date":          amzDate,
		"X-Amz-Expires":       strconv.Itoa(int(ttl.Seconds())),
		"X-Amz-SignedHeaders": signedHeaders,
	}

	canonicalRequest := strings.Join([]string{
		method,
		c.canonicalURI(key),
		canonicalQuery(query),
		canonicalHeaders(headers),
		signedHeaders,
		unsignedPayload,
	}, "\n")

	stringToSign := strings.Join([]string{
		algorithm,
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signature := hex.EncodeToString(hmacSHA256(c.signingKey(now), []byte(stringToSign)))
	query["X-Amz-Signature"] = signature

	return fmt.Sprintf("%s://%s%s?%s", c.scheme, c.requestHost(), c.canonicalURI(key), canonicalQuery(query)), nil
}

func (c *Client) signingKey(t time.Time) []byte {
	key := hmacSHA256([]byte("AWS4"+c.cfg.SecretKey), []byte(t.Format(dateFormat)))
	key = hmacSHA256(key, []byte(c.cfg.Region))
	key = hmacSHA256(key, []byte(service))
	return hmacSHA256(key, []byte("aws4_request"))
}

func canonicalQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, uriEncode(k)+"="+uriEncode(params[k]))
	}
	return strings.Join(parts, "&")
}

func canonicalHeaders(headers map[string]string) string {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(strings.TrimSpace(headers[k]))
		b.WriteByte('\n')
	}
	return b.String()
}

// encodePath encodes each path segment while preserving the separators.
func encodePath(key string) string {
	segments := strings.Split(key, "/")
	for i, s := range segments {
		segments[i] = uriEncode(s)
	}
	return strings.Join(segments, "/")
}

// uriEncode implements the RFC 3986 encoding AWS requires, which differs from
// url.QueryEscape in that spaces become %20 and ~ stays literal.
func uriEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' || ch == '~':
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func responseError(op, key string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("s3 %s %q: %s: %s", op, key, resp.Status, strings.TrimSpace(string(body)))
}
