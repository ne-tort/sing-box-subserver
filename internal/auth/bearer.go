package auth

import "net/http"

// Bearer verifies Authorization: Bearer <token> against a single secret.
func Bearer(r *http.Request, want string) bool {
	return tokenEQ(ExtractBearer(r), want)
}
