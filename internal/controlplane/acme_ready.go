//go:build with_controlplane

package controlplane

import (
	"os"
	"path/filepath"
	"strings"
)

// acmeCertificateReady reports whether certmagic has issued PEMs for every domain.
// Storage layout (certmagic): {acmeDir}/certificates/<issuer>/<name>/<name>.crt
func acmeCertificateReady(dataDir string, domains []string) (ready bool, missing []string, found []string) {
	if len(domains) == 0 {
		return false, nil, nil
	}
	root := filepath.Join(acmeDataDirectory(dataDir), "certificates")
	missing = make([]string, 0)
	found = make([]string, 0, len(domains))
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if acmeDomainHasCert(root, d) {
			found = append(found, d)
			continue
		}
		missing = append(missing, d)
	}
	return len(missing) == 0 && len(found) > 0, missing, found
}

func acmeDomainHasCert(certificatesRoot, domain string) bool {
	entries, err := os.ReadDir(certificatesRoot)
	if err != nil {
		return false
	}
	for _, iss := range entries {
		if !iss.IsDir() {
			continue
		}
		exactCert := filepath.Join(certificatesRoot, iss.Name(), domain, domain+".crt")
		if st, err := os.Stat(exactCert); err == nil && st.Size() > 0 {
			return true
		}
		dir := filepath.Join(certificatesRoot, iss.Name(), domain)
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			name := strings.ToLower(f.Name())
			if strings.HasSuffix(name, ".crt") {
				full := filepath.Join(dir, f.Name())
				if st, err := os.Stat(full); err == nil && st.Size() > 0 {
					return true
				}
			}
		}
	}
	return false
}

// acmeCertKeyPaths returns certmagic PEM paths for domain under certificatesRoot.
func acmeCertKeyPaths(certificatesRoot, domain string) (certPath, keyPath string, ok bool) {
	entries, err := os.ReadDir(certificatesRoot)
	if err != nil {
		return "", "", false
	}
	for _, iss := range entries {
		if !iss.IsDir() {
			continue
		}
		exactCert := filepath.Join(certificatesRoot, iss.Name(), domain, domain+".crt")
		exactKey := filepath.Join(certificatesRoot, iss.Name(), domain, domain+".key")
		if st, err := os.Stat(exactCert); err == nil && st.Size() > 0 {
			if _, err := os.Stat(exactKey); err == nil {
				return exactCert, exactKey, true
			}
		}
		dir := filepath.Join(certificatesRoot, iss.Name(), domain)
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var crt, key string
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			name := strings.ToLower(f.Name())
			full := filepath.Join(dir, f.Name())
			if strings.HasSuffix(name, ".crt") && crt == "" {
				crt = full
			}
			if strings.HasSuffix(name, ".key") && key == "" {
				key = full
			}
		}
		if crt != "" && key != "" {
			return crt, key, true
		}
	}
	return "", "", false
}
