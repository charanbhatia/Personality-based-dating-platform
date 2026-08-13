// Package email sends transactional mail. The log sender is the default so the
// stack runs with no mail server attached.
package email

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
)

const (
	ModeLog  = "log"
	ModeSMTP = "smtp"
)

type Message struct {
	To      string
	Subject string
	Body    string
}

type Sender interface {
	Send(ctx context.Context, msg Message) error
}

type Config struct {
	Mode     string
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

func New(cfg Config, log *slog.Logger) Sender {
	if cfg.Mode == ModeSMTP && cfg.Host != "" {
		return &smtpSender{cfg: cfg}
	}
	return &logSender{log: log}
}

// logSender records what would have been sent. It never fails, so a missing
// mail server cannot dead letter password resets in development.
type logSender struct {
	log *slog.Logger
}

func (s *logSender) Send(_ context.Context, msg Message) error {
	s.log.Info("email not sent (EMAIL_MODE=log)", "to", msg.To, "subject", msg.Subject, "body", msg.Body)
	return nil
}

type smtpSender struct {
	cfg Config
}

func (s *smtpSender) Send(_ context.Context, msg Message) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)

	var auth smtp.Auth
	if s.cfg.Username != "" {
		auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	}

	payload := strings.Join([]string{
		"From: " + s.cfg.From,
		"To: " + msg.To,
		"Subject: " + msg.Subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		msg.Body,
	}, "\r\n")

	return smtp.SendMail(addr, auth, s.cfg.From, []string{msg.To}, []byte(payload))
}
