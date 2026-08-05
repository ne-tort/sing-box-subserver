//go:build with_controlplane

package controlplane

import (
	"context"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/freedns"
)

const (
	acmeObtainWaitDefault = 180 * time.Second
	acmeObtainPollEvery   = 3 * time.Second
)

// certManagerInIPMode reports whether CM is configured for bare-IP ACME.
func certManagerInIPMode(cm domain.CertManager) bool {
	domains := cm.NormalizedDomains()
	if len(domains) == 0 {
		return false
	}
	for _, d := range domains {
		if net.ParseIP(d) == nil {
			return false
		}
	}
	return true
}

func mergeDomainLists(base, extra []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(base)+len(extra))
	for _, list := range [][]string{base, extra} {
		for _, d := range list {
			d = strings.ToLower(strings.TrimSpace(d))
			if d == "" {
				continue
			}
			if _, ok := seen[d]; ok {
				continue
			}
			seen[d] = struct{}{}
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Service) acmeEmailFromConfig() string {
	if s == nil || s.cfg.Cfg == nil {
		return ""
	}
	return strings.TrimSpace(s.cfg.Cfg.Controlplane.AcmeEmail)
}

func (s *Service) resolveBootstrapIPv4(ctx context.Context) (net.IP, error) {
	host := ""
	if s.cfg.Cfg != nil {
		host = strings.TrimSpace(s.cfg.Cfg.Controlplane.PublicHost)
	}
	return freedns.ResolvePublicIPv4(ctx, host)
}

// ensureAutoFreeDNSAndACME registers free-DNS names and merges them into cert-manager.
// wait>0 blocks until ACME PEMs appear (or timeout), then shrinks domains to found set.
// Soft-skips provider/ACME failures; never fails the agent boot hard.
func (s *Service) ensureAutoFreeDNSAndACME(ctx context.Context, wait time.Duration) map[string]any {
	out := map[string]any{
		"free_dns": nil,
		"acme":     map[string]any{"attempted": false},
	}
	if s == nil || s.store == nil {
		return out
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ip, err := s.resolveBootstrapIPv4(ctx)
	if err != nil {
		if s.log != nil {
			s.log.Debug("free-dns: skip (no public ipv4)", "err", err)
		}
		out["skipped"] = "no_public_ipv4"
		return out
	}

	st, err := freedns.Ensure(ctx, freedns.Options{DataDir: s.cfg.DataDir, IPv4: ip})
	if err != nil && s.log != nil {
		s.log.Warn("free-dns: ensure state save failed", "err", err)
	}
	out["free_dns"] = st.Payload()
	hosts := st.Hosts()
	if len(hosts) == 0 {
		out["skipped"] = "no_hosts"
		return out
	}

	cm, err := s.ensureCertManager()
	if err != nil {
		if s.log != nil {
			s.log.Warn("free-dns: load cert-manager failed", "err", err)
		}
		return out
	}

	before := cm.NormalizedDomains()
	merged := mergeDomainLists(before, hosts)
	email := strings.TrimSpace(cm.Email)
	if email == "" {
		email = s.acmeEmailFromConfig()
	}
	cm.Domains = merged
	if email != "" {
		cm.Email = email
	}
	if cm.Provider == "" {
		cm.Provider = "letsencrypt"
	}

	// Persist hosts even without email (SNI bank); ACME issue needs email.
	if err := cm.Validate(); err != nil {
		// Empty email with domains fails Validate — save domains without enabling ACME path via empty email strip.
		if email == "" {
			if s.log != nil {
				s.log.Info("free-dns: hosts ready, acme email missing — skip issue", "hosts", hosts)
			}
			// Store domains only when we have email; otherwise keep free_dns.json as source of truth.
			out["acme"] = map[string]any{"attempted": false, "reason": "acme_email_missing", "hosts": hosts}
			return out
		}
		if s.log != nil {
			s.log.Warn("free-dns: cert-manager validate failed", "err", err)
		}
		out["acme"] = map[string]any{"attempted": false, "reason": err.Error()}
		return out
	}

	same := strings.Join(before, ",") == strings.Join(merged, ",")
	if !same || !cm.Enabled() {
		if err := s.store.SaveCertManager(cm); err != nil {
			if s.log != nil {
				s.log.Warn("free-dns: save cert-manager failed", "err", err)
			}
			return out
		}
		s.noteACMEModeEntered()
		s.mgmtTLS.mu.Lock()
		s.mgmtTLS.cert = nil
		s.mgmtTLS.mu.Unlock()
		if err := s.rematerializeForce(ctx, true); err != nil && s.log != nil {
			s.log.Warn("free-dns: rematerialize after cert-manager update", "err", err)
		}
	}
	// Domains unchanged: do not rematerialize — that closes the DNS router mid-ACME obtain.

	acmeInfo := map[string]any{
		"attempted": true,
		"domains":   merged,
		"email_set": email != "",
	}
	if wait <= 0 || email == "" {
		ready, missing, found := acmeCertificateReady(s.cfg.DataDir, merged)
		acmeInfo["ready"] = ready
		acmeInfo["found"] = found
		acmeInfo["missing"] = missing
		out["acme"] = acmeInfo
		return out
	}

	ready, missing, found := s.waitACMECerts(ctx, merged, wait)
	acmeInfo["ready"] = ready
	acmeInfo["found"] = found
	acmeInfo["missing"] = missing

	// Shrink to found domains if partial success (avoid perpetual failed SAN).
	if len(found) > 0 && len(missing) > 0 {
		shrunk := mergeDomainLists(nil, found)
		// Keep any manual domains that already had certs or were not auto.
		manualKeep := make([]string, 0)
		autoSet := map[string]struct{}{}
		for _, h := range hosts {
			autoSet[h] = struct{}{}
		}
		for _, d := range before {
			if _, isAuto := autoSet[d]; !isAuto {
				manualKeep = append(manualKeep, d)
			}
		}
		cm.Domains = mergeDomainLists(manualKeep, shrunk)
		if err := cm.Validate(); err == nil {
			_ = s.store.SaveCertManager(cm)
			_ = s.rematerializeForce(ctx, true)
			acmeInfo["shrunk_to"] = cm.NormalizedDomains()
		}
	} else if len(found) == 0 && len(merged) > 1 {
		// Multi-SAN failed entirely — try each auto host alone.
		kept := s.tryACMEPerDomain(ctx, cm, before, hosts, email, wait/time.Duration(len(hosts)+1))
		acmeInfo["per_domain_kept"] = kept
		if len(kept) > 0 {
			ready2, miss2, found2 := acmeCertificateReady(s.cfg.DataDir, kept)
			acmeInfo["ready"] = ready2
			acmeInfo["found"] = found2
			acmeInfo["missing"] = miss2
		}
	}
	s.noteACMEReady(acmeInfo["ready"] == true)
	out["acme"] = acmeInfo
	return out
}

func (s *Service) waitACMECerts(ctx context.Context, domains []string, wait time.Duration) (ready bool, missing, found []string) {
	deadline := time.Now().Add(wait)
	for {
		ready, missing, found = acmeCertificateReady(s.cfg.DataDir, domains)
		if ready {
			return ready, missing, found
		}
		// Partial: at least one cert and we've waited a bit — allow early exit if all auto obtained eventually.
		if len(found) > 0 && time.Now().After(deadline.Add(-wait/3)) {
			// keep polling until deadline for the rest
		}
		if time.Now().After(deadline) {
			return ready, missing, found
		}
		select {
		case <-ctx.Done():
			return acmeCertificateReady(s.cfg.DataDir, domains)
		case <-time.After(acmeObtainPollEvery):
		}
	}
}

func (s *Service) tryACMEPerDomain(ctx context.Context, base domain.CertManager, manual, auto []string, email string, perWait time.Duration) []string {
	if perWait < 30*time.Second {
		perWait = 30 * time.Second
	}
	kept := append([]string{}, manual...)
	for _, h := range auto {
		cm := base
		cm.Email = email
		cm.Domains = mergeDomainLists(manual, []string{h})
		cm.Provider = "letsencrypt"
		if err := cm.Validate(); err != nil {
			continue
		}
		if err := s.store.SaveCertManager(cm); err != nil {
			continue
		}
		s.noteACMEModeEntered()
		_ = s.rematerializeForce(ctx, true)
		ready, _, found := s.waitACMECerts(ctx, []string{h}, perWait)
		if ready || len(found) > 0 {
			kept = mergeDomainLists(kept, []string{h})
		}
	}
	if len(kept) == 0 {
		return nil
	}
	cm := base
	cm.Email = email
	cm.Domains = kept
	cm.Provider = "letsencrypt"
	if err := cm.Validate(); err == nil {
		_ = s.store.SaveCertManager(cm)
		_ = s.rematerializeForce(ctx, true)
	}
	return kept
}
