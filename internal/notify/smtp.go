package notify

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
)

// SMTPSender sends email via standard SMTP using net/smtp.SendMail.
type SMTPSender struct {
	host     string
	port     int
	username string
	password string
	from     string
}

// NewSMTPSender creates an SMTPSender.
func NewSMTPSender(host string, port int, username, password, from string) *SMTPSender {
	return &SMTPSender{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
	}
}

// stripCRLF removes carriage-return and line-feed characters to prevent header injection.
func stripCRLF(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}

// SendEmail sends a minimal RFC 2822 message via SMTP.
// The context is accepted for interface compliance but net/smtp.SendMail does
// not support context cancellation — callers should rely on the engine's
// 30-second dispatch timeout.
func (s *SMTPSender) SendEmail(_ context.Context, to, subject, body string) error {
	to = stripCRLF(to)
	subject = stripCRLF(subject)
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	msg := fmt.Sprintf("To: %s\r\nFrom: %s\r\nSubject: %s\r\n\r\n%s",
		to, s.from, subject, body)
	if err := smtp.SendMail(addr, auth, s.from, []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	return nil
}
