package notify

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

func newDefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// checkHTTPResponse returns a formatted error on a non-2xx status, consuming
// up to 512 bytes of the body for the message. Returns nil without touching
// the body on success so callers can still decode the response.
func checkHTTPResponse(resp *http.Response, provider string) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("%s: HTTP %d: %s", provider, resp.StatusCode, errBody)
}
