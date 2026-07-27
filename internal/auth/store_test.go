package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStore_CRUDAndAuthorize(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "bootstrap-secret-16+")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if s.Authorize(req) {
		t.Fatal("no header")
	}
	req.Header.Set("Authorization", "Bearer bootstrap-secret-16+")
	if !s.Authorize(req) {
		t.Fatal("bootstrap should work")
	}

	view, secret, err := s.Create("panel", "")
	if err != nil {
		t.Fatal(err)
	}
	if view.ID == "" || len(secret) < 16 {
		t.Fatalf("bad create: %+v %q", view, secret)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("Authorization", "Bearer "+secret)
	if !s.Authorize(req2) {
		t.Fatal("managed token")
	}

	list := s.List()
	if len(list) < 2 {
		t.Fatalf("list=%+v", list)
	}

	if err := s.DisableBootstrap(); err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer bootstrap-secret-16+")
	if s.Authorize(req) {
		t.Fatal("bootstrap should be off")
	}
	if !s.Authorize(req2) {
		t.Fatal("managed still works")
	}

	if err := s.Delete(view.ID); err == nil {
		t.Fatal("expected last credential error")
	} else if err != ErrLastCredential {
		t.Fatalf("want ErrLastCredential got %v", err)
	}

	_, secret2, err := s.Rotate("panel-next", true)
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("Authorization", "Bearer "+secret)
	if s.Authorize(req2) {
		t.Fatal("old revoked")
	}
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.Header.Set("Authorization", "Bearer "+secret2)
	if !s.Authorize(req3) {
		t.Fatal("rotated token")
	}

	s2, err := Open(dir, "bootstrap-secret-16+")
	if err != nil {
		t.Fatal(err)
	}
	if s2.BootstrapEnabled() {
		t.Fatal("bootstrap disabled persisted")
	}
	if !s2.Authorize(req3) {
		t.Fatal("persisted managed")
	}
	if _, err := os.Stat(filepath.Join(dir, credentialsFile)); err != nil {
		t.Fatal(err)
	}
}

func TestStore_CreateExplicitToken(t *testing.T) {
	s, err := Open(t.TempDir(), "bootstrap-secret-16+")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Create("x", "short")
	if err != ErrInvalidToken {
		t.Fatalf("got %v", err)
	}
	view, sec, err := s.Create("panel", "explicit-token-value!!")
	if err != nil || sec != "explicit-token-value!!" || view.Name != "panel" {
		t.Fatalf("%v %q %+v", err, sec, view)
	}
}
