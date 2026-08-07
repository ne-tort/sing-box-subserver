//go:build with_controlplane && !(with_lx_utls && with_utls)

package controlplane

// utlsFingerprintChoices is a no-op without with_lx_utls; schema keeps ref enum.
func utlsFingerprintChoices() []string { return nil }
