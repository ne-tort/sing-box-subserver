package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Bearer extracts and verifies Authorization: Bearer <token>.
func Bearer(r *http.Request, want string) bool {
	if want == "" {
		return false
	}
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	got := strings.TrimSpace(h[len(prefix):])
	if len(got) != len(want) {
		// still compare to avoid leaking length via early return timing on empty
		return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
