package notify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MailgunSender sends email via the Mailgun Messages API.
type MailgunSender struct {
	apiKey string
	domain string
	from   string
	client *http.Client
}

// NewMailgunSender creates a MailgunSender.
func NewMailgunSender(apiKey, domain, from string) *MailgunSender {
	return &MailgunSender{
		apiKey: apiKey,
		domain: domain,
		from:   from,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *MailgunSender) SendEmail(ctx context.Context, to, subject, body string) error {
	endpoint := fmt.Sprintf("https://api.mailgun.net/v3/%s/messages", s.domain)

	form := url.Values{}
	form.Set("from", s.from)
	form.Set("to", to)
	form.Set("subject", subject)
	form.Set("text", body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("mailgun: building request: %w", err)
	}
	req.SetBasicAuth("api", s.apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("mailgun: request: %w", err)
	}
	defer resp.Body.Close()

	return checkHTTPResponse(resp, "mailgun")
}
