//go:build with_controlplane

package controlplane

import (
	"net/http"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/freedns"
)

func (s *Service) sslProfilePayload(p domain.SSLProfile) map[string]any {
	p = p.Normalize()
	st := s.computeSSLStatus(p)
	out := map[string]any{
		"id":                          p.ID,
		"name":                        p.Name,
		"type":                        p.Type,
		"domain":                      p.Domain,
		"ip":                          p.IP,
		"email":                       p.Email,
		"email_auto":                  p.ACMEEmailAuto(),
		"provider":                    p.Provider,
		"key_type":                    p.KeyType,
		"disable_http_challenge":      p.DisableHTTPChallenge,
		"disable_tls_alpn_challenge":  p.DisableTLSALPNChallenge,
		"dns01_challenge":             redactDNS01(p.DNS01Challenge),
		"external_account":            redactDNS01(p.ExternalAccount),
		"acme_profile":                p.ACMEProfile,
		"default_server_name":         p.DefaultServerName,
		"alpn":                        p.ALPN,
		"min_version":                 p.MinVersion,
		"max_version":                 p.MaxVersion,
		"cipher_suites":               p.CipherSuites,
		"curve_preferences":           p.CurvePreferences,
		"self_signed_valid_days":      p.SelfSignedValidDays,
		"ech_enabled":                 p.ECHEnabled,
		"ech_sni":                     p.ECHSNI,
		"source":                      p.Source,
		"created_at":                  p.CreatedAt,
		"updated_at":                  p.UpdatedAt,
		"status":                      st,
	}
	if p.IsACME() {
		out["email_effective"] = s.effectiveACMEEmail(p)
	}
	return out
}

func (s *Service) realitySNIPool() []string {
	if s == nil {
		return domain.DefaultRealitySNIs()
	}
	if rc, err := s.loadRealityConfig(); err == nil {
		out := make([]string, 0, len(rc.Profiles))
		for _, ep := range rc.Profiles {
			if sn := strings.TrimSpace(ep.SNI); sn != "" {
				out = append(out, sn)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return domain.DefaultRealitySNIs()
}

func (s *Service) effectiveACMEEmail(p domain.SSLProfile) string {
	return domain.EffectiveACMEEmail(p, s.realitySNIPool())
}

func (s *Service) sslProfilesWithResolvedACMEEmail(list []domain.SSLProfile) []domain.SSLProfile {
	pool := s.realitySNIPool()
	out := make([]domain.SSLProfile, len(list))
	copy(out, list)
	for i := range out {
		if out[i].IsACME() {
			out[i].Email = domain.EffectiveACMEEmail(out[i], pool)
		}
	}
	return out
}

// redactDNS01 returns a shallow copy with sensitive values redacted for API responses.
func redactDNS01(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "key") || strings.Contains(lk, "secret") || strings.Contains(lk, "token") ||
			strings.Contains(lk, "password") || strings.Contains(lk, "mac") {
			out[k] = "[redacted]"
			continue
		}
		out[k] = v
	}
	return out
}

func (s *Service) handleSSLList(w http.ResponseWriter, r *http.Request) {
	_ = s.ensureSSLProfiles()
	// Ensure Reality pool exists so options.reality_snis is never empty on boot.
	_, _ = s.loadRealityConfig()
	list, err := s.loadSSLProfiles()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	profiles := make([]any, 0, len(list))
	for _, p := range list {
		// Refresh ACME leaf copy for status.
		if p.IsACME() {
			_ = s.syncACMEIssuedToLeaf(p)
		}
		profiles = append(profiles, s.sslProfilePayload(p))
	}
	payload := map[string]any{
		"profiles": profiles,
		"options":  s.sslFieldOptions(),
	}
	if fd, err := freedns.LoadState(s.cfg.DataDir); err == nil {
		payload["free_dns"] = fd.Payload()
	}
	okJSONETag(w, r, payload)
}

func (s *Service) handleSSLCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		failJSON(w, 400, "bad_request", "name required")
		return
	}
	now := time.Now().UTC()
	p := domain.SSLProfile{
		ID:        newSSLProfileID(),
		Name:      name,
		Type:      domain.SSLTypeSelfSigned,
		CreatedAt: now,
		UpdatedAt: now,
	}.Normalize()
	if err := s.upsertSSLProfile(p); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	p, _ = s.ensureSSLProfileMaterial(p, false)
	_ = s.upsertSSLProfile(p)
	okJSON(w, 201, s.sslProfilePayload(p))
}

func (s *Service) handleSSLGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	p, ok, err := s.findSSLProfile(id)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if !ok {
		failJSON(w, 404, "not_found", "ssl profile not found")
		return
	}
	if p.IsACME() {
		_ = s.syncACMEIssuedToLeaf(p)
	}
	okJSONETag(w, r, s.sslProfilePayload(p))
}

func (s *Service) handleSSLPut(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	existing, ok, err := s.findSSLProfile(id)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if !ok {
		failJSON(w, 404, "not_found", "ssl profile not found")
		return
	}
	var body domain.SSLProfile
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	body.ID = id
	body.CreatedAt = existing.CreatedAt
	if body.CreatedAt.IsZero() {
		body.CreatedAt = time.Now().UTC()
	}
	body.UpdatedAt = time.Now().UTC()
	if strings.TrimSpace(body.Name) == "" {
		body.Name = existing.Name
	}
	body = body.Normalize()
	// Soft validate: allow empty domain on draft self_signed until user sets identity.
	if body.Type == domain.SSLTypeSelfSigned && body.Domain == "" {
		// ok for draft
	} else if err := body.Validate(); err != nil {
		failJSON(w, 400, "cp_invalid_ssl_profile", err.Error())
		return
	}
	// Rotate leaf when type/identity changes so status/expiry never come from a stale cert.
	if sslProfileIdentityChanged(existing, body) {
		if body.IsACME() || existing.IsACME() {
			_ = s.clearSSLACMEMaterial(body.ID)
		} else {
			s.clearSSLLeaf(body.ID)
		}
	}
	body, err = s.ensureSSLProfileMaterial(body, false)
	if err != nil {
		failJSON(w, 422, "cp_ssl_material_failed", err.Error())
		return
	}
	if err := s.upsertSSLProfile(body); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if err := s.rematerializeForce(r.Context(), true); err != nil {
		failJSON(w, 422, materializeErrorCode(err), err.Error())
		return
	}
	// Re-sync ACME leaf after rematerialize (obtain may have completed).
	if body.IsACME() {
		_ = s.syncACMEIssuedToLeaf(body)
	}
	okJSON(w, 200, s.sslProfilePayload(body))
}

func (s *Service) handleSSLDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == defaultSSLProfileID {
		failJSON(w, 422, "cp_ssl_delete_refused", "cannot delete default ssl profile")
		return
	}
	_, ok, err := s.findSSLProfile(id)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if !ok {
		failJSON(w, 404, "not_found", "ssl profile not found")
		return
	}
	ref, err := s.sslProfileReferenced(id)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if ref {
		failJSON(w, 422, "cp_ssl_in_use", "ssl profile is referenced by an inbound binding")
		return
	}
	if err := s.deleteSSLProfileFiles(id); err != nil {
		code := 422
		if strings.Contains(err.Error(), "permission denied") {
			code = 403
		}
		failJSON(w, code, "cp_ssl_delete_failed", err.Error())
		return
	}
	if err := s.removeSSLProfile(id); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	_ = s.rematerializeForce(r.Context(), true)
	okJSON(w, 200, map[string]any{"ok": true, "id": id})
}

func (s *Service) handleSSLRegenerate(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	p, ok, err := s.findSSLProfile(id)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if !ok {
		failJSON(w, 404, "not_found", "ssl profile not found")
		return
	}
	p = p.Normalize()
	p.UpdatedAt = time.Now().UTC()
	// ACME: wipe leaf + ACME store so rematerialize triggers a fresh obtain.
	if p.IsACME() {
		if err := s.clearSSLACMEMaterial(p.ID); err != nil {
			failJSON(w, 422, "cp_ssl_material_failed", err.Error())
			return
		}
	}
	p, err = s.ensureSSLProfileMaterial(p, true)
	if err != nil {
		failJSON(w, 422, "cp_ssl_material_failed", err.Error())
		return
	}
	if err := s.upsertSSLProfile(p); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if err := s.rematerializeForce(r.Context(), true); err != nil {
		failJSON(w, 422, materializeErrorCode(err), err.Error())
		return
	}
	okJSON(w, 200, s.sslProfilePayload(p))
}
