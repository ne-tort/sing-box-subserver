//go:build with_controlplane

package domain

import "testing"

func TestBindingUsesRealityCustomTLSMode(t *testing.T) {
	p := ProtocolPreset{
		CustomPreset: true,
		Traits:       []string{"tls", "custom"},
		ParamMeta: map[string]ParamFieldMeta{
			"tls_mode": {Default: "tls"},
		},
	}
	if BindingUsesReality(p, map[string]string{}) {
		t.Fatal("default tls_mode=tls must not require Reality")
	}
	if !BindingUsesReality(p, map[string]string{"tls_mode": "reality"}) {
		t.Fatal("tls_mode=reality must require Reality")
	}
	if BindingNeedsPEMTLS(p, map[string]string{"tls_mode": "none"}) {
		t.Fatal("tls_mode=none must not need PEM TLS")
	}
	if !BindingNeedsPEMTLS(p, nil) {
		t.Fatal("default tls must need PEM TLS")
	}
}

func TestBindingUsesRealityStockTrait(t *testing.T) {
	p := ProtocolPreset{Traits: []string{"reality", "tls"}}
	if !BindingUsesReality(p, nil) {
		t.Fatal("stock reality trait")
	}
}
