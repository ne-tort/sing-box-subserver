//go:build with_controlplane

package controlplane

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

const defaultSSLProfileID = "default"

// ensureSSLProfiles ensures the Default self_signed profile exists and PEMs are ready.
func (s *Service) ensureSSLProfiles() error {
	if s == nil || s.store == nil {
		return nil
	}
	if err := os.MkdirAll(sslRoot(s.cfg.DataDir), 0o700); err != nil {
		return err
	}
	list, err := s.loadSSLProfiles()
	if err != nil {
		return err
	}
	byID := map[string]domain.SSLProfile{}
	for _, p := range list {
		p = p.Normalize()
		byID[p.ID] = p
	}
	changed := false

	if _, ok := byID[defaultSSLProfileID]; !ok {
		host := "localhost"
		if s.cfg.Cfg != nil && s.cfg.Cfg.Controlplane.PublicHost != "" {
			host = s.cfg.Cfg.Controlplane.PublicHost
		}
		now := time.Now().UTC()
		p := domain.SSLProfile{
			ID:        defaultSSLProfileID,
			Name:      "Default",
			Type:      domain.SSLTypeSelfSigned,
			Domain:    host,
			CreatedAt: now,
			UpdatedAt: now,
		}.Normalize()
		byID[p.ID] = p
		changed = true
	}

	out := make([]domain.SSLProfile, 0, len(byID))
	for _, p := range byID {
		out = append(out, p)
	}
	if changed {
		if err := s.saveSSLProfiles(out); err != nil {
			return err
		}
	}

	for i := range out {
		p, err := s.ensureSSLProfileMaterial(out[i], false)
		if err != nil && s.log != nil {
			s.log.Warn("ssl profile material ensure failed", "id", p.ID, "err", err)
			continue
		}
		out[i] = p
	}
	if changed {
		_ = s.saveSSLProfiles(out)
	}
	return nil
}

func (s *Service) defaultSSLProfileID() string {
	return defaultSSLProfileID
}

func (s *Service) resolveSSLProfileForBinding(params map[string]string) (domain.SSLProfile, error) {
	id := ""
	if params != nil {
		id = strings.TrimSpace(params[domain.BindingParamSSLProfile])
	}
	if id == "" {
		id = s.defaultSSLProfileID()
	}
	p, ok, err := s.findSSLProfile(id)
	if err != nil {
		return domain.SSLProfile{}, err
	}
	if !ok {
		return domain.SSLProfile{}, fmt.Errorf("ssl profile %q not found", id)
	}
	return p, nil
}
