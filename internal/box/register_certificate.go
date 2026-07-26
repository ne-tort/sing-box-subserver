//go:build with_acme

package box

import (
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/service/acme"
	originca "github.com/sagernet/sing-box/service/origin_ca"
)

func CertificateProviderRegistry() *certificate.Registry {
	registry := certificate.NewRegistry()
	acme.RegisterCertificateProvider(registry)
	originca.RegisterCertificateProvider(registry)
	return registry
}
