//go:build with_controlplane

package wgawg

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"strings"
)

// DeviceParams is AWG2 junk/padding/headers (no i1–i5).
type DeviceParams struct {
	JC   int    `json:"jc"`
	JMin int    `json:"jmin"`
	JMax int    `json:"jmax"`
	S1   int    `json:"s1"`
	S2   int    `json:"s2"`
	S3   int    `json:"s3"`
	S4   int    `json:"s4"`
	H1   string `json:"h1"`
	H2   string `json:"h2"`
	H3   string `json:"h3"`
	H4   string `json:"h4"`
}

// AWG3Params is header protection + timings (SPEC 039).
type AWG3Params struct {
	HeaderProtectionKey     string `json:"header_protection_key"`
	ContentPaddingAddition  string `json:"content_padding_addition"`
	RekeyAfterTime          string `json:"rekey_after_time"`
	RekeyTimeout            string `json:"rekey_timeout"`
	RejectAfterTime         string `json:"reject_after_time"`
	KeepaliveTimeout        string `json:"keepalive_timeout"`
	MaxHandshakeAttempts    string `json:"max_handshake_attempts"`
}

// MasqueradeParams is sugar id/ip/ib (lx generates CPS at runtime).
type MasqueradeParams struct {
	ID string `json:"id"`
	IP string `json:"ip"` // quic|dns|stun|sip
	IB string `json:"ib"` // chrome|firefox|curl
}

var decoyDomains = []string{
	"nic.at", "nic.ch", "switch.ch", "restena.lu", "denic.de", "nic.cz",
	"uio.no", "helsinki.fi", "uu.se", "ku.dk", "tum.de", "rwth-aachen.de",
	"kit.edu", "fu-berlin.de", "univie.ac.at", "unige.ch", "polimi.it",
	"upm.es", "jaist.ac.jp", "nict.go.jp", "kaist.ac.kr", "ntu.edu.tw",
	"cuhk.edu.hk", "nus.edu.sg", "anu.edu.au", "ualberta.ca", "mcgill.ca",
	"usp.br", "unam.mx", "bnf.fr", "dnb.de", "bl.uk",
}

var masqueradeIPs = []string{"quic", "dns", "stun", "sip"}
var masqueradeIBs = []string{"chrome", "firefox", "curl"}

func rnd(lo, hi int) int {
	if hi < lo {
		lo, hi = hi, lo
	}
	n := hi - lo + 1
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return lo
	}
	return lo + int(v.Int64())
}

func rRange(base, spread, maxEnd int) string {
	start := base + rnd(0, spread)
	if start > maxEnd {
		start = maxEnd
	}
	end := start + rnd(1000, 50_000)
	if end > maxEnd {
		end = maxEnd
	}
	if end < start {
		end = start
	}
	if start == end {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}

// GenerateDeviceParams mirrors frontend awgGenerate.generateAwgDeviceParams.
func GenerateDeviceParams(awg3 bool) DeviceParams {
	const maxH = 4_294_967_295
	zone := maxH / 5
	spread := zone - 10_000
	if spread > 100_000_000 {
		spread = 100_000_000
	}
	sMin := 1
	if awg3 {
		sMin = 12
	}
	s1 := rnd(sMin, 150)
	s2 := rnd(sMin, 150)
	for s2 == s1+56 {
		s2 = rnd(sMin, 150)
	}
	s3Max := 64
	if s3Max < sMin {
		s3Max = sMin
	}
	s3 := rnd(sMin, s3Max)
	for attempts := 0; attempts < 10 && (s3 == s1+56 || s3 == s2+92); attempts++ {
		s3 = rnd(sMin, s3Max)
	}
	s4Max := 32
	if s4Max < sMin {
		s4Max = sMin
	}
	s4 := rnd(sMin, s4Max)

	jc := rnd(3, 10)
	jmin := rnd(128, 512)
	jmax := rnd(512, 1024)
	minJmax := jmin + 64
	if jmax <= minJmax {
		jmax = minJmax + rnd(64, 256)
	}

	h1min, h1max := zone, zone*2-10_000
	h2min, h2max := zone*2, zone*3-10_000
	h3min, h3max := zone*3, zone*4-10_000
	h4min := zone * 4
	h4spread := spread
	if h4spread > 150_000_000 {
		h4spread = 150_000_000
	}

	return DeviceParams{
		JC: jc, JMin: jmin, JMax: jmax,
		S1: s1, S2: s2, S3: s3, S4: s4,
		H1: rRange(rnd(h1min, h1max), spread, maxH),
		H2: rRange(rnd(h2min, h2max), spread, maxH),
		H3: rRange(rnd(h3min, h3max), spread, maxH),
		H4: rRange(rnd(h4min, maxH), h4spread, maxH),
	}
}

// GenerateAWG3Params mirrors frontend generateAwg3Params.
func GenerateAWG3Params() (AWG3Params, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return AWG3Params{}, err
	}
	return AWG3Params{
		HeaderProtectionKey:    base64.StdEncoding.EncodeToString(key),
		ContentPaddingAddition: fmt.Sprintf("%d-%d", rnd(8, 24), rnd(32, 64)),
		RekeyAfterTime:         fmt.Sprintf("%d-%d", rnd(100, 120), rnd(130, 160)),
		RekeyTimeout:           fmt.Sprintf("%d-%d", rnd(4, 8), rnd(10, 16)),
		RejectAfterTime:        fmt.Sprintf("%d-%d", rnd(160, 180), rnd(190, 220)),
		KeepaliveTimeout:       fmt.Sprintf("%d-%d", rnd(8, 12), rnd(14, 20)),
		MaxHandshakeAttempts:   fmt.Sprintf("%d-%d", rnd(3, 5), rnd(6, 10)),
	}, nil
}

// GenerateMasquerade picks random decoy domain + protocol + browser.
func GenerateMasquerade() MasqueradeParams {
	return MasqueradeParams{
		ID: decoyDomains[rnd(0, len(decoyDomains)-1)],
		IP: masqueradeIPs[rnd(0, len(masqueradeIPs)-1)],
		IB: masqueradeIBs[rnd(0, len(masqueradeIBs)-1)],
	}
}

// Bundle merges device (+ optional AWG3) + masquerade into a flat map for hub.AWG.
func Bundle(awg3 bool) (map[string]any, error) {
	dev := GenerateDeviceParams(awg3)
	masq := GenerateMasquerade()
	out := map[string]any{
		"jc": dev.JC, "jmin": dev.JMin, "jmax": dev.JMax,
		"s1": dev.S1, "s2": dev.S2, "s3": dev.S3, "s4": dev.S4,
		"h1": dev.H1, "h2": dev.H2, "h3": dev.H3, "h4": dev.H4,
		"id": masq.ID, "ip": masq.IP, "ib": masq.IB,
	}
	if awg3 {
		p3, err := GenerateAWG3Params()
		if err != nil {
			return nil, err
		}
		// HP requires padding floors ≥12
		for _, k := range []string{"s1", "s2", "s3", "s4"} {
			n, _ := out[k].(int)
			if n < 12 {
				out[k] = rnd(12, 48)
			}
		}
		out["header_protection_key"] = p3.HeaderProtectionKey
		out["content_padding_addition"] = p3.ContentPaddingAddition
		out["rekey_after_time"] = p3.RekeyAfterTime
		out["rekey_timeout"] = p3.RekeyTimeout
		out["reject_after_time"] = p3.RejectAfterTime
		out["keepalive_timeout"] = p3.KeepaliveTimeout
		out["max_handshake_attempts"] = p3.MaxHandshakeAttempts
	}
	return out, nil
}

// ApplyToEndpoint copies AWG map onto a wireguard endpoint object.
// Masquerade sugar (id/ip/ib) and explicit CPS (i1–i5) are mutually exclusive:
// if any iN is set, sugar is omitted; otherwise sugar is applied and iN cleared.
func ApplyToEndpoint(ep map[string]any, awg map[string]any, profile string) {
	if ep == nil || len(awg) == 0 {
		return
	}
	keys := []string{"jc", "jmin", "jmax", "s1", "s2", "s3", "s4", "h1", "h2", "h3", "h4"}
	if profile == "wg_awg3" || profile == "awg3" {
		keys = append(keys,
			"header_protection_key", "content_padding_addition",
			"rekey_after_time", "rekey_timeout", "reject_after_time",
			"keepalive_timeout", "max_handshake_attempts",
		)
	}
	for _, k := range keys {
		if v, ok := awg[k]; ok {
			ep[k] = v
		}
	}
	manualCPS := false
	for _, k := range []string{"i1", "i2", "i3", "i4", "i5"} {
		if v, ok := awg[k]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
			manualCPS = true
			break
		}
	}
	if manualCPS {
		for _, k := range []string{"i1", "i2", "i3", "i4", "i5"} {
			if v, ok := awg[k]; ok {
				ep[k] = v
			} else {
				delete(ep, k)
			}
		}
		for _, k := range []string{"id", "ip", "ib"} {
			delete(ep, k)
		}
		return
	}
	for _, k := range []string{"id", "ip", "ib"} {
		if v, ok := awg[k]; ok {
			ep[k] = v
		}
	}
	for _, k := range []string{"i1", "i2", "i3", "i4", "i5"} {
		delete(ep, k)
	}
}
