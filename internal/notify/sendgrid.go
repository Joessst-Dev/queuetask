package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SendGridSender sends email via the SendGrid v3 Mail Send API.
type SendGridSender struct {
	apiKey string
	from   string
	client *http.Client
}

// NewSendGridSender creates a SendGridSender.
func NewSendGridSender(apiKey, from string) *SendGridSender {
	return &SendGridSender{
		apiKey: apiKey,
		from:   from,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *SendGridSender) SendEmail(ctx context.Context, to, subject, body string) error {
	payload := map[string]any{
		"personalizations": []map[string]any{
			{"to": []map[string]string{{"email": to}}},
		},
		"from":    map[string]string{"email": s.from},
		"subject": subject,
		"content": []map[string]string{
			{"type": "text/plain", "value": body},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("sendgrid: marshalling payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.sendgrid.com/v3/mail/send", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("sendgrid: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sendgrid: request: %w", err)
	}
	defer resp.Body.Close()

	return checkHTTPResponse(resp, "sendgrid")
}
