//go:build !with_controlplane

package agentcfg

import "fmt"

// CheckBindPolicy enforces NFR-5: public bind requires TLS or insecure_public_bind.
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
	return fmt.Errorf("agent config: listen %q is not loopback; set tls.cert/key or insecure_public_bind=true", c.Listen)
}
