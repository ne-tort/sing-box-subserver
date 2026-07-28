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
		// Exact leaf used by certmagic: .../<issuer>/<domain>/<domain>.crt
		exact := filepath.Join(certificatesRoot, iss.Name(), domain, domain+".crt")
		if st, err := os.Stat(exact); err == nil && st.Size() > 0 {
			return true
		}
		// Fallback: any .crt under .../<issuer>/<domain>/
		dir := filepath.Join(certificatesRoot, iss.Name(), domain)
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(strings.ToLower(f.Name()), ".crt") {
				return true
			}
		}
	}
	return false
}
