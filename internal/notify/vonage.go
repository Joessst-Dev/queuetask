package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// VonageSender sends SMS via the Vonage SMS REST API (formerly Nexmo).
type VonageSender struct {
	apiKey    string
	apiSecret string
	from      string
	client    *http.Client
}

// NewVonageSender creates a VonageSender.
func NewVonageSender(apiKey, apiSecret, from string) *VonageSender {
	return &VonageSender{
		apiKey:    apiKey,
		apiSecret: apiSecret,
		from:      from,
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *VonageSender) SendSMS(ctx context.Context, to, body string) error {
	form := url.Values{}
	form.Set("api_key", s.apiKey)
	form.Set("api_secret", s.apiSecret)
	form.Set("from", s.from)
	form.Set("to", to)
	form.Set("text", body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://rest.nexmo.com/sms/json", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("vonage: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("vonage: request: %w", err)
	}
	defer resp.Body.Close()

	if err := checkHTTPResponse(resp, "vonage"); err != nil {
		return err
	}
	// Vonage always returns HTTP 200, even for rejected messages.
	// Inspect the per-message status field to detect actual delivery failures.
	var result struct {
		Messages []struct {
			Status    string `json:"status"`
			ErrorText string `json:"error-text"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result); err != nil {
		return fmt.Errorf("vonage: decoding response: %w", err)
	}
	if len(result.Messages) == 0 {
		return fmt.Errorf("vonage: empty messages array in response")
	}
	if result.Messages[0].Status != "0" {
		return fmt.Errorf("vonage: message rejected (status %s): %s",
			result.Messages[0].Status, result.Messages[0].ErrorText)
	}
	return nil
}
