package subscribe_test

import (
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/subscribe"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	plain := []byte(`{"inbounds":[],"outbounds":[{"type":"direct","tag":"d"}]}`)
	tok := "agent-secret"
	enc, err := subscribe.EncryptBody(plain, tok)
	if err != nil {
		t.Fatal(err)
	}
	out, err := subscribe.MaybeDecrypt(enc, tok, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(plain) {
		t.Fatalf("got %s", out)
	}
	if _, err := subscribe.MaybeDecrypt(enc, "wrong", true); err == nil {
		t.Fatal("expected decrypt fail")
	}
}

func TestMaybeDecryptPlainPassthrough(t *testing.T) {
	plain := []byte(`{"inbounds":[],"outbounds":[]}`)
	out, err := subscribe.MaybeDecrypt(plain, "tok", false)
	if err != nil || string(out) != string(plain) {
		t.Fatalf("out=%s err=%v", out, err)
	}
	if _, err := subscribe.MaybeDecrypt(plain, "tok", true); err == nil {
		t.Fatal("requireEnc should fail on plain")
	}
}
