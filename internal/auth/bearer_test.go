package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearer(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if Bearer(r, "secret") {
		t.Fatal("missing header")
	}
	r.Header.Set("Authorization", "Bearer secret")
	if !Bearer(r, "secret") {
		t.Fatal("expected ok")
	}
	r.Header.Set("Authorization", "Bearer wrong")
	if Bearer(r, "secret") {
		t.Fatal("expected reject")
	}
}
