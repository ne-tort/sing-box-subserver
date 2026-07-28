//go:build with_controlplane

package controlplane

import (
	"testing"
	"time"
)

func TestShouldACMEFallbackObtainTimeout(t *testing.T) {
	s := &Service{}
	s.noteACMEModeEntered()
	s.acmeWatch.mu.Lock()
	s.acmeWatch.enteredAt = time.Now().Add(-acmeObtainGrace - time.Second)
	s.acmeWatch.mu.Unlock()
	ok, reason := s.shouldACMEFallback()
	if !ok || reason == "" {
		t.Fatalf("expected obtain timeout fallback, got ok=%v reason=%q", ok, reason)
	}
}

func TestShouldACMEFallbackLostAfterReady(t *testing.T) {
	s := &Service{}
	s.noteACMEModeEntered()
	s.noteACMEReady(true)
	s.noteACMEReady(false)
	s.acmeWatch.mu.Lock()
	s.acmeWatch.lostSince = time.Now().Add(-acmeLostGrace - time.Second)
	s.acmeWatch.mu.Unlock()
	ok, reason := s.shouldACMEFallback()
	if !ok || reason == "" {
		t.Fatalf("expected lost-cert fallback, got ok=%v reason=%q", ok, reason)
	}
}

func TestShouldACMEFallbackWithinGrace(t *testing.T) {
	s := &Service{}
	s.noteACMEModeEntered()
	ok, _ := s.shouldACMEFallback()
	if ok {
		t.Fatal("should not fallback within obtain grace")
	}
}
