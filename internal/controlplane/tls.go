//go:build with_controlplane

package controlplane

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func sanitizeSNIFile(sni string) string {
	s := strings.ToLower(strings.TrimSpace(sni))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "slot"
	}
	if len(out) > 120 {
		out = out[:120]
	}
	return out
}

// writeSelfSignedPair writes a leaf cert/key pair (and fingerprint meta) for SSL profiles.
func writeSelfSignedPair(certPath, keyPath, metaPath string, spec domain.SelfSignedSpec, fp string) (string, string, error) {
	if err := spec.Validate(); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return "", "", err
	}
	days := spec.ValidDays
	if days <= 0 {
		days = 3650
	}
	keyType := spec.KeyType
	if keyType == "" {
		keyType = "p256"
	}
	var (
		priv any
		pub  any
	)
	switch keyType {
	case "p256":
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return "", "", err
		}
		priv, pub = k, &k.PublicKey
	case "p384":
		k, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			return "", "", err
		}
		priv, pub = k, &k.PublicKey
	case "rsa2048":
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return "", "", err
		}
		priv, pub = k, &k.PublicKey
	default:
		return "", "", fmt.Errorf("unsupported key_type %q", keyType)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return "", "", err
	}
	org := spec.Organization
	if org == "" {
		org = "sing-box-subserver-controlplane"
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: spec.CommonName, Organization: []string{org}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Duration(days) * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     append([]string{}, spec.DNSSANs...),
	}
	if keyType == "rsa2048" {
		tmpl.KeyUsage |= x509.KeyUsageKeyEncipherment
	}
	for _, s := range spec.IPSANs {
		if ip := net.ParseIP(s); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		return "", "", err
	}
	if err := writePEM(certPath, "CERTIFICATE", der); err != nil {
		return "", "", err
	}
	var keyPEM []byte
	var keyTypeHdr string
	switch k := priv.(type) {
	case *ecdsa.PrivateKey:
		keyPEM, err = x509.MarshalECPrivateKey(k)
		keyTypeHdr = "EC PRIVATE KEY"
	case *rsa.PrivateKey:
		keyPEM = x509.MarshalPKCS1PrivateKey(k)
		keyTypeHdr = "RSA PRIVATE KEY"
	}
	if err != nil {
		return "", "", err
	}
	if err := writePEM(keyPath, keyTypeHdr, keyPEM); err != nil {
		return "", "", err
	}
	_ = os.WriteFile(metaPath, []byte(fp+"\n"), 0o600)
	return certPath, keyPath, nil
}

func fingerprintSpec(spec domain.SelfSignedSpec) string {
	return fmt.Sprintf("%s|%v|%v|%s|%d|%s",
		spec.CommonName, spec.DNSSANs, spec.IPSANs, spec.KeyType, spec.ValidDays, spec.Organization)
}

func writePEM(path, typ string, der []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: typ, Bytes: der})
}

func sslSlotDir(dataDir, sni string) string {
	return filepath.Join(sslRoot(dataDir), "_slots", sanitizeSNIFile(sni))
}

// ensureSlotSelfSigned writes a dedicated self-signed leaf for demux_sni differentiation.
// Path: controlplane/ssl/_slots/<sni>/cert.crt|key — CN/SAN match ClientHello SNI.
func ensureSlotSelfSigned(dataDir, sni string) (certPath, keyPath string, changed bool, err error) {
	sni = strings.ToLower(strings.TrimSpace(sni))
	if sni == "" || net.ParseIP(sni) != nil {
		return "", "", false, fmt.Errorf("slot sni must be a non-empty domain")
	}
	if strings.HasSuffix(sni, ".local") {
		return "", "", false, fmt.Errorf("slot sni must not use .local suffix")
	}
	dir := sslSlotDir(dataDir, sni)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", false, err
	}
	certPath = filepath.Join(dir, "cert.crt")
	keyPath = filepath.Join(dir, "cert.key")
	metaPath := certPath + ".meta"
	fp := "slot|" + sni + "|p256|3650"
	if _, err1 := os.Stat(certPath); err1 == nil {
		if _, err2 := os.Stat(keyPath); err2 == nil {
			if raw, err := os.ReadFile(metaPath); err == nil && string(raw) == fp+"\n" {
				return certPath, keyPath, false, nil
			}
		}
	}
	spec := domain.SelfSignedSpec{
		CommonName: sni,
		DNSSANs:    []string{sni},
		KeyType:    "p256",
		ValidDays:  3650,
	}
	certPath, keyPath, err = writeSelfSignedPair(certPath, keyPath, metaPath, spec, fp)
	if err != nil {
		return "", "", false, err
	}
	return certPath, keyPath, true, nil
}
