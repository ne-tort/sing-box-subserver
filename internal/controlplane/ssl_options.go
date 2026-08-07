//go:build with_controlplane

package controlplane

import (
	"context"
	"net"
	"os"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/freedns"
)

// ZeroSSL is gated: expose only after live issuance is confirmed (see sslZeroSSLEnabled).
const sslZeroSSLEnabled = false

func (s *Service) sslFieldOptions() map[string]any {
	providers := []string{"letsencrypt"}
	if sslZeroSSLEnabled {
		providers = append(providers, "zerossl")
	}

	domains := []string{}
	ips := []string{}
	if s != nil && s.cfg.DataDir != "" {
		if st, err := freedns.LoadState(s.cfg.DataDir); err == nil {
			domains = st.Hosts()
			if ip := strings.TrimSpace(st.IPv4); ip != "" {
				if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
					ips = []string{parsed.To4().String()}
				}
			}
		}
	}
	if len(ips) == 0 && s != nil && s.cfg.Cfg != nil {
		h := strings.TrimSpace(s.cfg.Cfg.Controlplane.PublicHost)
		if parsed := net.ParseIP(h); parsed != nil && parsed.To4() != nil {
			ips = []string{parsed.To4().String()}
		}
	}
	// Last resort: resolve public host / detect outbound IPv4 (same as free-DNS bootstrap).
	if len(ips) == 0 && s != nil {
		if ip, err := s.resolveBootstrapIPv4(context.Background()); err == nil && ip != nil {
			if v4 := ip.To4(); v4 != nil {
				ips = []string{v4.String()}
			}
		}
	}

	snis := []string{}
	if s != nil {
		snis = s.realitySNIPool()
	}
	if len(snis) == 0 {
		snis = domain.DefaultRealitySNIs()
	}

	return map[string]any{
		"types": []string{
			domain.SSLTypeSelfSigned,
			domain.SSLTypeACME,
			domain.SSLTypeACMEIP,
		},
		"providers":         providers,
		"domains":           domains,
		"ips":               ips,
		"reality_snis":      snis,
		"alpn":              []string{"h2", "http/1.1", "h3"},
		"min_version":       []string{"1.2", "1.3"},
		"max_version":       []string{"1.2", "1.3"},
		"cipher_suites":     sslDefaultCipherSuites(),
		"curve_preferences": []string{"X25519", "P256", "P384", "P521"},
	}
}

func sslDefaultCipherSuites() []string {
	return []string{
		"TLS_AES_128_GCM_SHA256",
		"TLS_AES_256_GCM_SHA384",
		"TLS_CHACHA20_POLY1305_SHA256",
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
	}
}

// clearSSLACMEMaterial removes leaf PEM and ACME data so the next rematerialize
// forces a fresh ACME obtain (regenerate).
func (s *Service) clearSSLACMEMaterial(id string) error {
	if s == nil || s.cfg.DataDir == "" {
		return nil
	}
	certPath, keyPath := sslCertPaths(s.cfg.DataDir, id)
	_ = os.Remove(certPath)
	_ = os.Remove(keyPath)
	_ = os.Remove(certPath + ".meta")
	acmeDir := sslACMEDir(s.cfg.DataDir, id)
	if err := os.RemoveAll(acmeDir); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(acmeDir, 0o700); err != nil {
		return err
	}
	return nil
}
