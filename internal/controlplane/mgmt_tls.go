//go:build with_controlplane

package controlplane

import (
	"crypto/tls"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

const (
	acmeObtainGrace  = 5 * time.Minute
	acmeLostGrace    = 2 * time.Minute
	mgmtCertCacheTTL = 5 * time.Second
)

// ServingHTTPS reports that the management API / subscription listener uses TLS.
// Always true when the controlplane module is compiled in.
func (s *Service) ServingHTTPS() bool {
	return s != nil
}

// GetCertificate selects the active management certificate from the Default SSL profile.
func (s *Service) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if s == nil {
		return nil, fmt.Errorf("controlplane unavailable")
	}
	certPath, keyPath, source, err := s.mgmtMaterialPaths()
	if err != nil {
		return nil, err
	}
	return s.loadMgmtCertificate(certPath, keyPath, source)
}

func (s *Service) mgmtMaterialPaths() (certPath, keyPath, source string, err error) {
	_ = s.ensureSSLProfiles()
	p, ok, err := s.findSSLProfile(defaultSSLProfileID)
	if err != nil {
		return "", "", "", err
	}
	if !ok {
		return "", "", "", fmt.Errorf("default ssl profile missing")
	}
	p, err = s.ensureSSLProfileMaterial(p, false)
	if err != nil {
		return "", "", "", err
	}
	certPath, keyPath = sslCertPaths(s.cfg.DataDir, p.ID)
	if _, err := os.Stat(certPath); err != nil {
		return "", "", "", fmt.Errorf("default ssl cert: %w", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		return "", "", "", fmt.Errorf("default ssl key: %w", err)
	}
	if p.IsACME() {
		st := s.computeSSLStatus(p)
		if st.State == domain.SSLStateReady {
			return certPath, keyPath, "ssl_acme", nil
		}
		return certPath, keyPath, "ssl_self_signed_interim", nil
	}
	return certPath, keyPath, "ssl_self_signed", nil
}

type mgmtCertCache struct {
	mu     sync.Mutex
	cert   *tls.Certificate
	path   string
	mod    time.Time
	source string
	loaded time.Time
}

func (s *Service) loadMgmtCertificate(certPath, keyPath, source string) (*tls.Certificate, error) {
	s.mgmtTLS.mu.Lock()
	defer s.mgmtTLS.mu.Unlock()

	st, err := os.Stat(certPath)
	if err != nil {
		return nil, fmt.Errorf("mgmt cert %s: %w", certPath, err)
	}
	if s.mgmtTLS.cert != nil && s.mgmtTLS.path == certPath && s.mgmtTLS.mod.Equal(st.ModTime()) &&
		time.Since(s.mgmtTLS.loaded) < mgmtCertCacheTTL {
		return s.mgmtTLS.cert, nil
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load mgmt tls (%s): %w", source, err)
	}
	s.mgmtTLS.cert = &pair
	s.mgmtTLS.path = certPath
	s.mgmtTLS.mod = st.ModTime()
	s.mgmtTLS.source = source
	s.mgmtTLS.loaded = time.Now()
	return s.mgmtTLS.cert, nil
}

func (s *Service) mgmtCertSource() string {
	s.mgmtTLS.mu.Lock()
	defer s.mgmtTLS.mu.Unlock()
	if s.mgmtTLS.source == "" {
		return "unknown"
	}
	return s.mgmtTLS.source
}

func (s *Service) noteACMEModeEntered() {
	s.acmeWatch.mu.Lock()
	defer s.acmeWatch.mu.Unlock()
	s.acmeWatch.enteredAt = time.Now()
	s.acmeWatch.everReady = false
	s.acmeWatch.lostSince = time.Time{}
}

func (s *Service) noteACMEReady(ready bool) {
	s.acmeWatch.mu.Lock()
	defer s.acmeWatch.mu.Unlock()
	if ready {
		s.acmeWatch.everReady = true
		s.acmeWatch.lostSince = time.Time{}
		return
	}
	if s.acmeWatch.everReady && s.acmeWatch.lostSince.IsZero() {
		s.acmeWatch.lostSince = time.Now()
	}
}

func (s *Service) shouldACMEFallback() (bool, string) {
	s.acmeWatch.mu.Lock()
	defer s.acmeWatch.mu.Unlock()
	if s.acmeWatch.enteredAt.IsZero() {
		s.acmeWatch.enteredAt = time.Now()
	}
	if !s.acmeWatch.everReady {
		if time.Since(s.acmeWatch.enteredAt) > acmeObtainGrace {
			return true, "acme obtain timeout: no certificate within grace period"
		}
		return false, ""
	}
	if !s.acmeWatch.lostSince.IsZero() && time.Since(s.acmeWatch.lostSince) > acmeLostGrace {
		return true, "acme certificate lost after successful obtain (renewal/issue failure)"
	}
	return false, ""
}
