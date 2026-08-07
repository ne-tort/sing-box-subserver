//go:build with_controlplane

package controlplane

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func sslRoot(dataDir string) string {
	return filepath.Join(dataDir, "controlplane", "ssl")
}

func sslProfileDir(dataDir, id string) string {
	return filepath.Join(sslRoot(dataDir), sanitizeSNIFile(id))
}

func sslCertPaths(dataDir, id string) (certPath, keyPath string) {
	dir := sslProfileDir(dataDir, id)
	return filepath.Join(dir, "cert.crt"), filepath.Join(dir, "cert.key")
}

func sslACMEDir(dataDir, id string) string {
	return filepath.Join(sslProfileDir(dataDir, id), "acme")
}

func sslECHPaths(dataDir, id string) (keyPath, configPath string) {
	dir := sslProfileDir(dataDir, id)
	return filepath.Join(dir, "ech.key.pem"), filepath.Join(dir, "ech.config.pem")
}

func newSSLProfileID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *Service) loadSSLProfiles() ([]domain.SSLProfile, error) {
	f, ok, err := s.store.LoadSSLProfiles()
	if err != nil {
		return nil, err
	}
	if !ok {
		return []domain.SSLProfile{}, nil
	}
	return f.Profiles, nil
}

func (s *Service) saveSSLProfiles(list []domain.SSLProfile) error {
	return s.store.SaveSSLProfiles(domain.SSLProfilesFile{Profiles: list})
}

func (s *Service) findSSLProfile(id string) (domain.SSLProfile, bool, error) {
	id = strings.TrimSpace(id)
	list, err := s.loadSSLProfiles()
	if err != nil {
		return domain.SSLProfile{}, false, err
	}
	for _, p := range list {
		if p.ID == id {
			return p, true, nil
		}
	}
	return domain.SSLProfile{}, false, nil
}

func (s *Service) upsertSSLProfile(p domain.SSLProfile) error {
	list, err := s.loadSSLProfiles()
	if err != nil {
		return err
	}
	found := false
	for i := range list {
		if list[i].ID == p.ID {
			list[i] = p
			found = true
			break
		}
	}
	if !found {
		list = append(list, p)
	}
	return s.saveSSLProfiles(list)
}

func (s *Service) removeSSLProfile(id string) error {
	list, err := s.loadSSLProfiles()
	if err != nil {
		return err
	}
	out := list[:0]
	for _, p := range list {
		if p.ID == id {
			continue
		}
		out = append(out, p)
	}
	return s.saveSSLProfiles(out)
}

func (s *Service) computeSSLStatus(p domain.SSLProfile) domain.SSLProfileStatus {
	st := domain.SSLProfileStatus{}
	certPath, keyPath := sslCertPaths(s.cfg.DataDir, p.ID)
	st.CertPath = certPath
	st.KeyPath = keyPath
	st.IsIP = p.Type == domain.SSLTypeACMEIP || net.ParseIP(p.ServerName()) != nil

	echKey, echCfg := sslECHPaths(s.cfg.DataDir, p.ID)
	if p.ECHEnabled {
		if _, e1 := os.Stat(echKey); e1 == nil {
			if _, e2 := os.Stat(echCfg); e2 == nil {
				st.ECHReady = true
			}
		}
	}

	raw, err := os.ReadFile(certPath)
	if err != nil {
		if os.IsNotExist(err) {
			if p.IsACME() {
				st.State = domain.SSLStatePending
			} else {
				st.State = domain.SSLStateMissing
			}
			return st
		}
		if os.IsPermission(err) {
			st.State = domain.SSLStateError
			st.LastError = fmt.Sprintf("permission denied reading %s", certPath)
			return st
		}
		st.State = domain.SSLStateError
		st.LastError = err.Error()
		return st
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		rest := raw
		for {
			var b *pem.Block
			b, rest = pem.Decode(rest)
			if b == nil {
				break
			}
			if b.Type == "CERTIFICATE" {
				block = b
				break
			}
		}
	}
	if block == nil {
		st.State = domain.SSLStateError
		st.LastError = "no CERTIFICATE PEM block"
		return st
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		st.State = domain.SSLStateError
		st.LastError = err.Error()
		return st
	}
	// Stale leaf after type/identity change (e.g. old self-signed while profile is ACME).
	if !sslLeafMatchesProfile(cert, p) {
		if p.IsACME() {
			st.State = domain.SSLStatePending
			return st
		}
		st.State = domain.SSLStateMissing
		return st
	}
	nb, na := cert.NotBefore.UTC(), cert.NotAfter.UTC()
	st.NotBefore = &nb
	st.NotAfter = &na
	st.SubjectCN = cert.Subject.CommonName
	st.SANs = append([]string{}, cert.DNSNames...)
	for _, ip := range cert.IPAddresses {
		st.SANs = append(st.SANs, ip.String())
	}
	st.Issuer = cert.Issuer.CommonName
	if time.Now().UTC().After(na) {
		st.State = domain.SSLStateExpired
		return st
	}
	if _, err := os.Stat(keyPath); err != nil {
		st.State = domain.SSLStateError
		st.LastError = "certificate present but key missing"
		return st
	}
	st.State = domain.SSLStateReady
	return st
}

func sslLeafMatchesProfile(cert *x509.Certificate, p domain.SSLProfile) bool {
	want := strings.ToLower(strings.TrimSpace(p.ServerName()))
	if want == "" {
		return true
	}
	if p.Type == domain.SSLTypeACMEIP {
		ip := net.ParseIP(want)
		if ip == nil {
			return false
		}
		for _, a := range cert.IPAddresses {
			if a.Equal(ip) {
				return true
			}
		}
		// Some ACME IP leaves put the IP in CN.
		if strings.EqualFold(strings.TrimSpace(cert.Subject.CommonName), want) {
			return true
		}
		return false
	}
	cn := strings.ToLower(strings.TrimSpace(cert.Subject.CommonName))
	if cn == want {
		return true
	}
	for _, d := range cert.DNSNames {
		if strings.ToLower(strings.TrimSpace(d)) == want {
			return true
		}
	}
	return false
}

func sslProfileIdentityChanged(prev, next domain.SSLProfile) bool {
	prev, next = prev.Normalize(), next.Normalize()
	if prev.Type != next.Type {
		return true
	}
	if prev.Domain != next.Domain {
		return true
	}
	if prev.IP != next.IP {
		return true
	}
	return false
}

// clearSSLLeaf removes only cert.crt/key/meta (keeps ACME account cache when possible).
func (s *Service) clearSSLLeaf(id string) {
	if s == nil || s.cfg.DataDir == "" {
		return
	}
	certPath, keyPath := sslCertPaths(s.cfg.DataDir, id)
	_ = os.Remove(certPath)
	_ = os.Remove(keyPath)
	_ = os.Remove(certPath + ".meta")
}

func (s *Service) ensureSSLProfileMaterial(p domain.SSLProfile, force bool) (domain.SSLProfile, error) {
	p = p.Normalize()
	if err := os.MkdirAll(sslProfileDir(s.cfg.DataDir, p.ID), 0o700); err != nil {
		return p, err
	}
	switch p.Type {
	case domain.SSLTypeSelfSigned:
		cn := p.Domain
		if cn == "" {
			cn = "localhost"
		}
		days := p.SelfSignedValidDays
		if days <= 0 {
			days = 3650
		}
		spec := domain.SelfSignedSpec{
			CommonName:   cn,
			DNSSANs:      []string{cn},
			KeyType:      "p256",
			ValidDays:    days,
			Organization: "sing-box-subserver-ssl",
		}
		if cn != "localhost" {
			spec.DNSSANs = []string{cn, "localhost"}
		}
		certPath, keyPath := sslCertPaths(s.cfg.DataDir, p.ID)
		metaPath := certPath + ".meta"
		fp := fmt.Sprintf("ssl|%s|%s|%d", p.ID, cn, days)
		need := force
		if !need {
			if _, e1 := os.Stat(certPath); e1 != nil {
				need = true
			} else if raw, e := os.ReadFile(metaPath); e != nil || string(raw) != fp+"\n" {
				need = true
			}
		}
		if need {
			if _, _, err := writeSelfSignedPair(certPath, keyPath, metaPath, spec, fp); err != nil {
				return p, err
			}
		}
	case domain.SSLTypeACME, domain.SSLTypeACMEIP:
		if err := os.MkdirAll(sslACMEDir(s.cfg.DataDir, p.ID), 0o700); err != nil {
			return p, err
		}
		// Drop stale leaf (e.g. previous self-signed) that does not match ACME identity.
		certPath, _ := sslCertPaths(s.cfg.DataDir, p.ID)
		if raw, err := os.ReadFile(certPath); err == nil {
			if block, _ := pem.Decode(raw); block != nil {
				if cert, err := x509.ParseCertificate(block.Bytes); err == nil && !sslLeafMatchesProfile(cert, p) {
					s.clearSSLLeaf(p.ID)
				}
			}
		} else if force {
			s.clearSSLLeaf(p.ID)
		}
		_ = s.syncACMEIssuedToLeaf(p)
	}
	if p.ECHEnabled {
		if err := s.ensureProfileECH(p, force); err != nil {
			return p, err
		}
	}
	return p, nil
}

func (s *Service) syncACMEIssuedToLeaf(p domain.SSLProfile) error {
	acmeDir := sslACMEDir(s.cfg.DataDir, p.ID)
	type cand struct {
		cert, key string
		mod       time.Time
		match     bool
	}
	var best *cand
	_ = filepath.Walk(acmeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		base := strings.ToLower(info.Name())
		if !strings.HasSuffix(base, ".crt") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil
		}
		keyPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".key"
		if _, err := os.Stat(keyPath); err != nil {
			// certmagic sometimes uses different key naming; keep empty
			keyPath = ""
		}
		c := cand{cert: path, key: keyPath, mod: info.ModTime(), match: sslLeafMatchesProfile(cert, p)}
		if best == nil {
			best = &c
			return nil
		}
		// Prefer identity match, then newest mtime.
		if c.match && !best.match {
			best = &c
			return nil
		}
		if c.match == best.match && c.mod.After(best.mod) {
			best = &c
		}
		return nil
	})
	if best == nil || !best.match {
		return nil
	}
	certPath, keyPath := sslCertPaths(s.cfg.DataDir, p.ID)
	raw, err := os.ReadFile(best.cert)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certPath, raw, 0o600); err != nil {
		return err
	}
	if best.key != "" {
		if kr, err := os.ReadFile(best.key); err == nil {
			_ = os.WriteFile(keyPath, kr, 0o600)
		}
	}
	return nil
}

func (s *Service) ensureProfileECH(p domain.SSLProfile, force bool) error {
	keyPath, cfgPath := sslECHPaths(s.cfg.DataDir, p.ID)
	if !force {
		if _, e1 := os.Stat(keyPath); e1 == nil {
			if _, e2 := os.Stat(cfgPath); e2 == nil {
				return nil
			}
		}
	}
	sni := strings.TrimSpace(p.ECHSNI)
	if sni == "" {
		sni = s.pickECHSNIFallback(p)
	}
	configPEM, keyPEM, err := echKeygenDefault(sni)
	if err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, []byte(keyPEM), 0o600); err != nil {
		return err
	}
	return os.WriteFile(cfgPath, []byte(configPEM), 0o600)
}

func (s *Service) pickECHSNIFallback(p domain.SSLProfile) string {
	if rc, err := s.loadRealityConfig(); err == nil {
		cands := make([]string, 0, len(rc.Profiles))
		for _, ep := range rc.Profiles {
			if ep.SNI != "" {
				cands = append(cands, ep.SNI)
			}
		}
		if len(cands) > 0 {
			var b [1]byte
			_, _ = rand.Read(b[:])
			return cands[int(b[0])%len(cands)]
		}
	}
	if sn := p.ServerName(); sn != "" && net.ParseIP(sn) == nil {
		return sn
	}
	if s.cfg.Cfg != nil && s.cfg.Cfg.Controlplane.PublicHost != "" {
		h := s.cfg.Cfg.Controlplane.PublicHost
		if net.ParseIP(h) == nil {
			return h
		}
	}
	return "localhost"
}

func (s *Service) deleteSSLProfileFiles(id string) error {
	dir := sslProfileDir(s.cfg.DataDir, id)
	err := os.RemoveAll(dir)
	if err == nil {
		return nil
	}
	if os.IsPermission(err) {
		return fmt.Errorf("cp_ssl_delete_failed: permission denied removing %s: %w", dir, err)
	}
	return fmt.Errorf("cp_ssl_delete_failed: %w", err)
}

func (s *Service) sslProfileReferenced(id string) (bool, error) {
	sets, err := s.store.LoadSets()
	if err != nil {
		return false, err
	}
	for _, set := range sets {
		for _, b := range set.EffectiveBindings() {
			if strings.TrimSpace(b.Params[domain.BindingParamSSLProfile]) == id {
				return true, nil
			}
		}
	}
	return false, nil
}
