package httputil

import (
	"crypto/tls"
	"net/http"
	"time"
)

// NewClient returns an HTTP client with the given timeout.
// When tlsInsecure is true, TLS certificate verification is skipped (local/dev only).
func NewClient(timeoutSec int, tlsInsecure bool) *http.Client {
	if timeoutSec <= 0 {
		timeoutSec = 15
	}
	c := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	if tlsInsecure {
		c.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // explicit opt-in for local panels
		}
	}
	return c
}
