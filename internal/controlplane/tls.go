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
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func tlsMaterialPaths(dataDir string) (certPath, keyPath string) {
	dir := filepath.Join(dataDir, "controlplane", "tls")
	return filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key")
}

func acmeDataDirectory(dataDir string) string {
	return filepath.Join(dataDir, "controlplane", "acme")
}

// ensureSelfSigned writes PEM according to spec. If force is false and files exist
// and fingerprint matches, files are reused. changed is true when PEM was written.
func ensureSelfSigned(dataDir string, spec domain.SelfSignedSpec, force bool) (certPath, keyPath string, changed bool, err error) {
	if err := spec.Validate(); err != nil {
		return "", "", false, err
	}
	dir := filepath.Join(dataDir, "controlplane", "tls")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", false, err
	}
	certPath, keyPath = tlsMaterialPaths(dataDir)
	metaPath := filepath.Join(dir, "self_signed.meta.json")
	fp := fingerprintSpec(spec)
	if !force {
		if _, err1 := os.Stat(certPath); err1 == nil {
			if _, err2 := os.Stat(keyPath); err2 == nil {
				if raw, err := os.ReadFile(metaPath); err == nil && string(raw) == fp+"\n" {
					return certPath, keyPath, false, nil
				}
			}
		}
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
			return "", "", false, err
		}
		priv, pub = k, &k.PublicKey
	case "p384":
		k, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			return "", "", false, err
		}
		priv, pub = k, &k.PublicKey
	case "rsa2048":
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return "", "", false, err
		}
		priv, pub = k, &k.PublicKey
	default:
		return "", "", false, fmt.Errorf("unsupported key_type %q", keyType)
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return "", "", false, err
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
		return "", "", false, err
	}
	if err := writePEM(certPath, "CERTIFICATE", der); err != nil {
		return "", "", false, err
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
		return "", "", false, err
	}
	if err := writePEM(keyPath, keyTypeHdr, keyPEM); err != nil {
		return "", "", false, err
	}
	_ = os.WriteFile(metaPath, []byte(fp+"\n"), 0o600)
	return certPath, keyPath, true, nil
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
