//go:build with_controlplane

package controlplane

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/freedns"
)

// Stable IDs for bootstrap-managed free-DNS / bare-IP SSL profiles.
// Fixed IDs guarantee reuse across reinstall without duplicating ACME accounts.
const (
	sslIDFreeDNSSSLip = "fd-sslip"
	sslIDFreeDNSNIP   = "fd-nip"
	sslIDFreeDNSAddr  = "fd-addrtools"
	sslIDFreeDNSIP    = "fd-ip"

	sslSourceFreeDNSSSLip = "free_dns:sslip"
	sslSourceFreeDNSNIP   = "free_dns:nip"
	sslSourceFreeDNSAddr  = "free_dns:addrtools"
	sslSourceFreeDNSIP    = "free_dns:ip"
)

type freeDNSJob struct {
	id     string
	name   string
	source string
	kind   string // acme | acme_ip
	domain string
	ip     string
}

// bootstrapFreeDNSSSL registers free-DNS hosts (soft-skip per provider) then ensures
// matching ACME / ACME-IP SSL profiles with stable IDs and no duplicates.
func (s *Service) bootstrapFreeDNSSSL(ctx context.Context) {
	if s == nil {
		return
	}
	s.ensureFreeDNS(ctx)
	s.ensureFreeDNSSSLProfiles(ctx)
}

// ensureFreeDNSSSLProfiles creates or reuses ACME profiles for free-DNS hosts + bare IP.
// Errors for a single provider/profile are soft-skipped (no retries).
func (s *Service) ensureFreeDNSSSLProfiles(ctx context.Context) {
	_ = ctx
	if s == nil || s.store == nil || s.cfg.DataDir == "" {
		return
	}
	_ = s.ensureSSLProfiles()

	st, err := freedns.LoadState(s.cfg.DataDir)
	if err != nil {
		if s.log != nil {
			s.log.Debug("free-dns ssl: load state skipped", "err", err)
		}
		return
	}

	list, err := s.loadSSLProfiles()
	if err != nil {
		if s.log != nil {
			s.log.Debug("free-dns ssl: load profiles skipped", "err", err)
		}
		return
	}
	changed := false

	jobs := make([]freeDNSJob, 0, 4)
	addHost := func(provider, id, name, source string) {
		p, ok := st.Providers[provider]
		if !ok || p.Status != freedns.StatusOK {
			return
		}
		host := strings.ToLower(strings.TrimSpace(p.Host))
		if host == "" {
			return
		}
		jobs = append(jobs, freeDNSJob{id: id, name: name, source: source, kind: domain.SSLTypeACME, domain: host})
	}
	addHost(freedns.ProviderSSLip, sslIDFreeDNSSSLip, "Free DNS (sslip)", sslSourceFreeDNSSSLip)
	addHost(freedns.ProviderNIP, sslIDFreeDNSNIP, "Free DNS (nip)", sslSourceFreeDNSNIP)
	addHost(freedns.ProviderAddrTools, sslIDFreeDNSAddr, "Free DNS (addr.tools)", sslSourceFreeDNSAddr)

	ipStr := strings.TrimSpace(st.IPv4)
	if ipStr == "" && s.cfg.Cfg != nil {
		ipStr = strings.TrimSpace(s.cfg.Cfg.Controlplane.PublicHost)
	}
	if ip := net.ParseIP(ipStr); ip != nil && ip.To4() != nil {
		jobs = append(jobs, freeDNSJob{
			id: sslIDFreeDNSIP, name: "Free DNS (IP)", source: sslSourceFreeDNSIP,
			kind: domain.SSLTypeACMEIP, ip: ip.To4().String(),
		})
	}

	// Adopt orphan dirs for known IDs even when provider is currently skipped.
	for _, id := range []string{sslIDFreeDNSSSLip, sslIDFreeDNSNIP, sslIDFreeDNSAddr, sslIDFreeDNSIP} {
		if profileByID(list, id) != nil {
			continue
		}
		if !sslProfileDirExists(s.cfg.DataDir, id) {
			continue
		}
		if jobHasID(jobs, id) {
			continue // ensureOneFreeDNSSSL will create/adopt via stable id path
		}
		if p, ok := s.inferOrphanFreeDNSProfile(id, st); ok {
			list = append(list, p)
			changed = true
			if s.log != nil {
				s.log.Info("free-dns ssl: adopted orphan profile", "id", id, "type", p.Type, "domain", p.Domain, "ip", p.IP)
			}
		}
	}

	for _, j := range jobs {
		next, did, err := s.ensureOneFreeDNSSSL(list, j)
		if err != nil {
			if s.log != nil {
				s.log.Debug("free-dns ssl: profile skipped", "id", j.id, "source", j.source, "err", err)
			}
			continue
		}
		list = next
		if did {
			changed = true
		}
	}

	if !changed {
		return
	}
	if err := s.saveSSLProfiles(list); err != nil {
		if s.log != nil {
			s.log.Debug("free-dns ssl: save skipped", "err", err)
		}
		return
	}
	for i := range list {
		if !strings.HasPrefix(list[i].Source, "free_dns:") && !isFreeDNSStableID(list[i].ID) {
			continue
		}
		p, err := s.ensureSSLProfileMaterial(list[i], false)
		if err != nil {
			if s.log != nil {
				s.log.Debug("free-dns ssl: material skipped", "id", list[i].ID, "err", err)
			}
			continue
		}
		list[i] = p
	}
	_ = s.saveSSLProfiles(list)
}

func jobHasID(jobs []freeDNSJob, id string) bool {
	for _, j := range jobs {
		if j.id == id {
			return true
		}
	}
	return false
}

func isFreeDNSStableID(id string) bool {
	switch id {
	case sslIDFreeDNSSSLip, sslIDFreeDNSNIP, sslIDFreeDNSAddr, sslIDFreeDNSIP:
		return true
	default:
		return false
	}
}

func profileByID(list []domain.SSLProfile, id string) *domain.SSLProfile {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}

func findACMEByDomain(list []domain.SSLProfile, domainName string) *domain.SSLProfile {
	d := strings.ToLower(strings.TrimSpace(domainName))
	if d == "" {
		return nil
	}
	for i := range list {
		p := list[i].Normalize()
		if p.Type == domain.SSLTypeACME && p.Domain == d {
			return &list[i]
		}
	}
	return nil
}

func findACMEIPByIP(list []domain.SSLProfile, ip string) *domain.SSLProfile {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}
	for i := range list {
		p := list[i].Normalize()
		if p.Type == domain.SSLTypeACMEIP && p.IP == ip {
			return &list[i]
		}
	}
	return nil
}

func sslProfileDirExists(dataDir, id string) bool {
	st, err := os.Stat(sslProfileDir(dataDir, id))
	return err == nil && st.IsDir()
}

func (s *Service) ensureOneFreeDNSSSL(list []domain.SSLProfile, j freeDNSJob) ([]domain.SSLProfile, bool, error) {
	now := time.Now().UTC()

	// 1) Stable id already in catalog → update identity if needed.
	if cur := profileByID(list, j.id); cur != nil {
		p := cur.Normalize()
		updated := false
		if p.Type != j.kind {
			p.Type = j.kind
			updated = true
		}
		if j.kind == domain.SSLTypeACME && j.domain != "" && p.Domain != j.domain {
			p.Domain = j.domain
			updated = true
		}
		if j.kind == domain.SSLTypeACMEIP && j.ip != "" && p.IP != j.ip {
			p.IP = j.ip
			updated = true
		}
		if p.Source == "" {
			p.Source = j.source
			updated = true
		}
		if p.Name == "" {
			p.Name = j.name
			updated = true
		}
		if !updated {
			return list, false, nil
		}
		p.UpdatedAt = now
		if err := p.Validate(); err != nil {
			return list, false, err
		}
		for i := range list {
			if list[i].ID == j.id {
				list[i] = p
				break
			}
		}
		return list, true, nil
	}

	// 2) Another profile already covers this identity → no duplicate.
	if j.kind == domain.SSLTypeACME {
		if other := findACMEByDomain(list, j.domain); other != nil {
			if s.log != nil {
				s.log.Debug("free-dns ssl: domain already covered", "domain", j.domain, "by", other.ID)
			}
			return list, false, nil
		}
	}
	if j.kind == domain.SSLTypeACMEIP {
		if other := findACMEIPByIP(list, j.ip); other != nil {
			if s.log != nil {
				s.log.Debug("free-dns ssl: ip already covered", "ip", j.ip, "by", other.ID)
			}
			return list, false, nil
		}
	}

	// 3) Create catalog entry (disk dir under stable id reused if present).
	p := domain.SSLProfile{
		ID:        j.id,
		Name:      j.name,
		Type:      j.kind,
		Domain:    j.domain,
		IP:        j.ip,
		Source:    j.source,
		Provider:  "letsencrypt",
		CreatedAt: now,
		UpdatedAt: now,
	}.Normalize()
	if err := p.Validate(); err != nil {
		return list, false, err
	}
	if _, err := s.ensureSSLProfileMaterial(p, false); err != nil && s.log != nil {
		s.log.Debug("free-dns ssl: material soft-fail", "id", j.id, "err", err)
	}
	list = append(list, p)
	if s.log != nil {
		s.log.Info("free-dns ssl: profile ensured", "id", j.id, "source", j.source, "domain", j.domain, "ip", j.ip)
	}
	return list, true, nil
}

func (s *Service) inferOrphanFreeDNSProfile(id string, st freedns.State) (domain.SSLProfile, bool) {
	now := time.Now().UTC()
	p := domain.SSLProfile{ID: id, CreatedAt: now, UpdatedAt: now, Provider: "letsencrypt"}
	switch id {
	case sslIDFreeDNSSSLip:
		p.Name, p.Source, p.Type = "Free DNS (sslip)", sslSourceFreeDNSSSLip, domain.SSLTypeACME
		if h := st.Providers[freedns.ProviderSSLip].Host; h != "" {
			p.Domain = strings.ToLower(strings.TrimSpace(h))
		}
	case sslIDFreeDNSNIP:
		p.Name, p.Source, p.Type = "Free DNS (nip)", sslSourceFreeDNSNIP, domain.SSLTypeACME
		if h := st.Providers[freedns.ProviderNIP].Host; h != "" {
			p.Domain = strings.ToLower(strings.TrimSpace(h))
		}
	case sslIDFreeDNSAddr:
		p.Name, p.Source, p.Type = "Free DNS (addr.tools)", sslSourceFreeDNSAddr, domain.SSLTypeACME
		if h := st.Providers[freedns.ProviderAddrTools].Host; h != "" {
			p.Domain = strings.ToLower(strings.TrimSpace(h))
		} else if st.AddrHost != "" {
			p.Domain = strings.ToLower(strings.TrimSpace(st.AddrHost))
		}
	case sslIDFreeDNSIP:
		p.Name, p.Source, p.Type = "Free DNS (IP)", sslSourceFreeDNSIP, domain.SSLTypeACMEIP
		if ip := net.ParseIP(st.IPv4); ip != nil && ip.To4() != nil {
			p.IP = ip.To4().String()
		}
	default:
		return domain.SSLProfile{}, false
	}
	p = p.Normalize()
	if p.Type == domain.SSLTypeACME && p.Domain == "" {
		if cn := readLeafCNOrSAN(s.cfg.DataDir, id); cn != "" && net.ParseIP(cn) == nil {
			p.Domain = cn
		}
	}
	if p.Type == domain.SSLTypeACMEIP && p.IP == "" {
		if cn := readLeafCNOrSAN(s.cfg.DataDir, id); cn != "" && net.ParseIP(cn) != nil {
			p.IP = cn
		}
	}
	if err := p.Validate(); err != nil {
		return domain.SSLProfile{}, false
	}
	return p, true
}

func readLeafCNOrSAN(dataDir, id string) string {
	certPath, _ := sslCertPaths(dataDir, id)
	raw, err := os.ReadFile(certPath)
	if err != nil {
		return ""
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return ""
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return ""
	}
	if cn := strings.ToLower(strings.TrimSpace(cert.Subject.CommonName)); cn != "" {
		return cn
	}
	for _, d := range cert.DNSNames {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			return d
		}
	}
	for _, ip := range cert.IPAddresses {
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}
