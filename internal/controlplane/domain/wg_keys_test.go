//go:build with_controlplane

package domain

import (
	"encoding/base64"
	"testing"
)

func TestNormalizeWireGuardKey_StdAndRawURL(t *testing.T) {
	t.Parallel()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	std := base64.StdEncoding.EncodeToString(raw)
	url := base64.RawURLEncoding.EncodeToString(raw)

	got, err := NormalizeWireGuardKey(url)
	if err != nil {
		t.Fatal(err)
	}
	if got != std {
		t.Fatalf("got %q want %q", got, std)
	}
	got2, err := NormalizeWireGuardKey(std)
	if err != nil || got2 != std {
		t.Fatalf("idempotent: %q %v", got2, err)
	}
	if _, err := base64.StdEncoding.DecodeString(got); err != nil {
		t.Fatal(err)
	}
}

func TestRandomWireGuardPrivate_StdDecodable(t *testing.T) {
	t.Parallel()
	for i := 0; i < 20; i++ {
		priv, err := RandomWireGuardPrivate()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := base64.StdEncoding.DecodeString(priv); err != nil {
			t.Fatalf("priv=%q: %v", priv, err)
		}
		pub, err := WireGuardPublicFromPrivate(priv)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := base64.StdEncoding.DecodeString(pub); err != nil {
			t.Fatalf("pub=%q: %v", pub, err)
		}
	}
}
