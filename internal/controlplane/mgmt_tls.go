//go:build with_controlplane

package controlplane

import (
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
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

// GetCertificate selects the active management certificate:
// ACME PEMs from cert-manager when issued; otherwise safety self_signed PEMs.
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
	p, err := s.ensureTLSProfile(false)
	if err != nil {
		return "", "", "", err
	}
	if err := s.ensureSafetySelfSignedPEMs(p); err != nil {
		return "", "", "", err
	}
	safetyCert, safetyKey := tlsMaterialPaths(s.cfg.DataDir)

	cm, err := s.ensureCertManager()
	if err != nil {
		return "", "", "", err
	}
	domains := cm.NormalizedDomains()
	if len(domains) > 0 {
		root := filepath.Join(acmeDataDirectory(s.cfg.DataDir), "certificates")
		if c, k, ok := acmeCertKeyPaths(root, domains[0]); ok {
			return c, k, "acme", nil
		}
		return safetyCert, safetyKey, "self_signed_interim", nil
	}
	return safetyCert, safetyKey, "self_signed", nil
}

func (s *Service) ensureSafetySelfSignedPEMs(p domain.TLSProfile) error {
	host := ""
	if s.cfg.Cfg != nil {
		host = s.cfg.Cfg.Controlplane.PublicHost
	}
	spec := p.SelfSigned
	if spec == nil {
		fallback := domain.DefaultSelfSigned(host)
		spec = fallback.SelfSigned
	}
	if spec == nil {
		return fmt.Errorf("no self_signed spec for safety PEMs")
	}
	_, _, _, err := ensureSelfSigned(s.cfg.DataDir, *spec, false)
	return err
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
