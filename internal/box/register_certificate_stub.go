//go:build !with_acme

package box

import (
	"github.com/sagernet/sing-box/adapter/certificate"
)

func CertificateProviderRegistry() *certificate.Registry {
	return certificate.NewRegistry()
}
