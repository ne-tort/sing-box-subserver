//go:build with_controlplane

package agentcfg

// CheckBindPolicy for controlplane builds: CP terminates management HTTPS via the
// active TLS profile (self_signed / ACME with self_signed emergency fallback).
// Explicit agent.yaml tls.cert/key or insecure_public_bind remain valid.
func (c *Config) CheckBindPolicy() error {
	if c.IsLoopbackListen() {
		return nil
	}
	if c.HasTLS() {
		return nil
	}
	if c.InsecurePublicBind {
		return nil
	}
	// Embedded controlplane always serves management TLS from CP material.
	return nil
}
