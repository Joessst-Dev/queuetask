package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TwilioSender sends SMS via the Twilio Messages API.
type TwilioSender struct {
	accountSID string
	authToken  string
	from       string
	client     *http.Client
}

// NewTwilioSender creates a TwilioSender.
func NewTwilioSender(accountSID, authToken, from string) *TwilioSender {
	return &TwilioSender{
		accountSID: accountSID,
		authToken:  authToken,
		from:       from,
		client:     &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *TwilioSender) SendSMS(ctx context.Context, to, body string) error {
	endpoint := fmt.Sprintf(
		"https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", s.accountSID)

	form := url.Values{}
	form.Set("To", to)
	form.Set("From", s.from)
	form.Set("Body", body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("twilio: building request: %w", err)
	}
	req.SetBasicAuth(s.accountSID, s.authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("twilio: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("twilio: HTTP %d: %s", resp.StatusCode, errBody)
	}
	return nil
}
