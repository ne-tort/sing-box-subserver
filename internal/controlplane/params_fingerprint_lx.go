//go:build with_controlplane && with_lx_utls && with_utls

package controlplane

import (
	"github.com/sagernet/sing-box/common/tls/lxutls"
)

// utlsFingerprintChoices returns selectable tls.utls.fingerprint values from the
// embedded FEATURE 018 lab catalog (same SoT as the binary handshake path).
func utlsFingerprintChoices() []string {
	doc, err := lxutls.GetDocument()
	if err != nil || doc == nil || len(doc.Fingerprints) == 0 {
		return nil
	}
	out := make([]string, len(doc.Fingerprints))
	copy(out, doc.Fingerprints)
	return out
}
