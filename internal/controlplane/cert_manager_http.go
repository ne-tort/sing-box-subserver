//go:build with_controlplane

package controlplane

import (
	"net/http"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func (s *Service) ensureCertManager() (domain.CertManager, error) {
	if s == nil || s.store == nil {
		return domain.CertManager{}, nil
	}
	cm, ok, err := s.store.LoadCertManager()
	if err != nil {
		return domain.CertManager{}, err
	}
	if ok {
		if err := cm.Validate(); err != nil {
			return domain.CertManager{}, err
		}
		return cm, nil
	}
	// One-shot migration from legacy tls_profile.json acme block.
	legacy, err := s.store.LoadLegacyACMEFromTLSProfile()
	if err != nil {
		return domain.CertManager{}, err
	}
	if legacy != nil {
		cm = *legacy
		cm.Domains = cm.NormalizedDomains()
		if err := cm.Validate(); err != nil {
			return domain.CertManager{}, err
		}
		if err := s.store.SaveCertManager(cm); err != nil {
			return domain.CertManager{}, err
		}
		// Rewrite TLS profile as self-signed only (drop mode/acme from disk).
		if _, err := s.ensureTLSProfile(true); err != nil {
			return domain.CertManager{}, err
		}
		s.noteACMEModeEntered()
		return cm, nil
	}
	return domain.CertManager{}, nil
}

func (s *Service) certManagerStatusPayload(cm domain.CertManager) map[string]any {
	domains := cm.NormalizedDomains()
	out := map[string]any{
		"email":                      cm.Email,
		"provider":                   cm.Provider,
		"domains":                    domains,
		"key_type":                   cm.KeyType,
		"disable_http_challenge":     cm.DisableHTTPChallenge,
		"disable_tls_alpn_challenge": cm.DisableTLSALPNChallenge,
		"alternative_http_port":      cm.AlternativeHTTPPort,
		"alternative_tls_port":       cm.AlternativeTLSPort,
		"dns01_challenge":            redactDNS01(cm.DNS01Challenge),
		"enabled":                    cm.Enabled(),
	}
	domainStatuses := make([]any, 0, len(domains))
	allReady := true
	var found, missing []string
	if len(domains) > 0 {
		ready, miss, fnd := acmeCertificateReady(s.cfg.DataDir, domains)
		found, missing = fnd, miss
		s.noteACMEReady(ready)
		allReady = ready
		missSet := map[string]struct{}{}
		for _, m := range miss {
			missSet[strings.ToLower(m)] = struct{}{}
		}
		for _, d := range domains {
			st := map[string]any{"domain": d, "status": "ready"}
			if _, ok := missSet[strings.ToLower(d)]; ok {
				st["status"] = "missing"
				st["reason"] = "certificate not found in acme data directory"
				allReady = false
			}
			domainStatuses = append(domainStatuses, st)
		}
	}
	ms := map[string]any{
		"ready":                    allReady || !cm.Enabled(),
		"acme_data_directory":      acmeDataDirectory(s.cfg.DataDir),
		"certificate_provider_tag": domain.TLSProviderTag,
		"acme_certs_found":         found,
		"acme_certs_missing":       missing,
	}
	if cm.Enabled() && !allReady {
		ms["ready_reason"] = "waiting for ACME obtain (certmagic)"
	}
	out["domain_status"] = domainStatuses
	out["material_status"] = ms
	return out
}

func redactDNS01(in map[string]any) any {
	if len(in) == 0 {
		return nil
	}
	red := map[string]any{}
	for k, v := range in {
		ks := strings.ToLower(k)
		if strings.Contains(ks, "token") || strings.Contains(ks, "secret") || strings.Contains(ks, "password") || strings.Contains(ks, "key") {
			red[k] = "[redacted]"
		} else {
			red[k] = v
		}
	}
	return red
}

func (s *Service) handleCertManagerGet(w http.ResponseWriter, _ *http.Request) {
	cm, err := s.ensureCertManager()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, s.certManagerStatusPayload(cm))
}

func (s *Service) handleCertManagerPut(w http.ResponseWriter, r *http.Request) {
	var cm domain.CertManager
	if err := decodeBody(r, &cm); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	cm.Domains = cm.NormalizedDomains()
	if err := cm.Validate(); err != nil {
		failJSON(w, 400, "cp_invalid_cert_manager", err.Error())
		return
	}
	if err := s.store.SaveCertManager(cm); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if cm.Enabled() {
		s.noteACMEModeEntered()
	} else {
		s.acmeWatch.mu.Lock()
		s.acmeWatch.enteredAt = time.Time{}
		s.acmeWatch.everReady = false
		s.acmeWatch.lostSince = time.Time{}
		s.acmeWatch.mu.Unlock()
	}
	s.mgmtTLS.mu.Lock()
	s.mgmtTLS.cert = nil
	s.mgmtTLS.mu.Unlock()
	if err := s.rematerializeForce(r.Context(), true); err != nil {
		failJSON(w, 422, materializeErrorCode(err), err.Error())
		return
	}
	okJSON(w, 200, s.certManagerStatusPayload(cm))
}
