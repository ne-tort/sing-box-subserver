package subscribe

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// EncryptedEnvelope is optional wire format for subscription / pull bodies.
// Key = SHA-256(agent token). Ciphertext is base64(nonce||ciphertext||tag).
type EncryptedEnvelope struct {
	Alg        string `json:"alg"` // "aes-256-gcm"
	Ciphertext string `json:"ciphertext"`
}

// EncryptBody encrypts plain config with agent token (for panels that want opaque URLs).
func EncryptBody(plain []byte, token string) ([]byte, error) {
	key := sha256.Sum256([]byte(token))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nonce, nonce, plain, nil)
	env := EncryptedEnvelope{
		Alg:        "aes-256-gcm",
		Ciphertext: base64.StdEncoding.EncodeToString(sealed),
	}
	return json.Marshal(env)
}

// MaybeDecrypt returns plaintext server JSON.
// - If body looks like EncryptedEnvelope → decrypt with token.
// - If requireEnc and body is plain → error.
// - Otherwise return body as-is.
func MaybeDecrypt(body []byte, token string, requireEnc bool) ([]byte, error) {
	trim := strings.TrimSpace(string(body))
	if !strings.HasPrefix(trim, "{") {
		if requireEnc {
			return nil, fmt.Errorf("encrypted body required")
		}
		return body, nil
	}
	var env EncryptedEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		if requireEnc {
			return nil, fmt.Errorf("encrypted body required: %w", err)
		}
		return body, nil
	}
	if !strings.EqualFold(env.Alg, "aes-256-gcm") || env.Ciphertext == "" {
		if requireEnc {
			return nil, fmt.Errorf("encrypted body required (alg=aes-256-gcm)")
		}
		// Likely plain sing-box JSON (has inbounds/outbounds etc.).
		return body, nil
	}
	raw, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt ciphertext: %w", err)
	}
	key := sha256.Sum256([]byte(token))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	plain, err := gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed (wrong token?): %w", err)
	}
	return plain, nil
}
