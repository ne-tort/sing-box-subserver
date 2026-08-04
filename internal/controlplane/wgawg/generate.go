//go:build with_controlplane

package wgawg

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
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

// GenerateMasquerade picks protocol + browser. ID must come from the Reality SNI
// pool on the client — never from a static decoy list.
func GenerateMasquerade() MasqueradeParams {
	return MasqueradeParams{
		ID: "",
		IP: masqueradeIPs[rnd(0, len(masqueradeIPs)-1)],
		IB: masqueradeIBs[rnd(0, len(masqueradeIBs)-1)],
	}
}

// GenerateInitPacket builds one AmneziaWG CPS string as a single <b 0xHEXBLOB>
// (official Amnezia / amneziawg-go style). Prefer GenerateInitPackets.
func GenerateInitPacket(minBytes, maxBytes int) string {
	if minBytes < 1 {
		minBytes = 1
	}
	if maxBytes < minBytes {
		maxBytes = minBytes
	}
	n := rnd(minBytes, maxBytes)
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return "<b 0x" + hex.EncodeToString(buf) + ">"
}

// GenerateInitPackets returns i1–i5 from the amnezia-wg-easy signatures bank,
// with protocol-aware entropy tags (<r>/<t>/…) applied on top of real dumps.
// preferred is a masquerade sugar hint (quic|dns|stun|sip|"") used to bias
// protocol family selection; empty / "none" picks across the full bank.
func GenerateInitPackets() (i1, i2, i3, i4, i5 string) {
	return GenerateInitPacketsPreferred("")
}

// GenerateInitPacketsPreferred is GenerateInitPackets with a protocol bias.
func GenerateInitPacketsPreferred(preferred string) (i1, i2, i3, i4, i5 string) {
	proto, _, slots, err := PickSignature(preferred)
	if err != nil {
		return fallbackInitPackets()
	}
	return RandomizeCPS(proto, slots.I1),
		RandomizeCPS(proto, slots.I2),
		RandomizeCPS(proto, slots.I3),
		RandomizeCPS(proto, slots.I4),
		RandomizeCPS(proto, slots.I5)
}

// GenerateInitPacketsDetailed returns slots plus the bank protocol/variant ids
// (useful for junk-range calibration and UI labels).
func GenerateInitPacketsDetailed(preferred string) (protocol, variant string, i1, i2, i3, i4, i5 string) {
	proto, ver, slots, err := PickSignature(preferred)
	if err != nil {
		a, b, c, d, e := fallbackInitPackets()
		return "fallback", "0", a, b, c, d, e
	}
	return proto, ver,
		RandomizeCPS(proto, slots.I1),
		RandomizeCPS(proto, slots.I2),
		RandomizeCPS(proto, slots.I3),
		RandomizeCPS(proto, slots.I4),
		RandomizeCPS(proto, slots.I5)
}

func fallbackInitPackets() (i1, i2, i3, i4, i5 string) {
	// Structured templates (not N×`<b 0xNN>`), matching amnezia-wg-easy fixtures.
	return "<b 0xc00000000108><r 8><b 0x000044d0><r 1232>",
		"<b 0x000100002112a442><r 12>",
		"<r 2><b 0x012000010000000000010000010001>",
		GenerateInitPacket(8, 24),
		GenerateInitPacket(8, 16)
}

// HasManualCPS reports whether awg carries explicit i1–i5.
func HasManualCPS(awg map[string]any) bool {
	if awg == nil {
		return false
	}
	for _, k := range []string{"i1", "i2", "i3", "i4", "i5"} {
		if v, ok := awg[k]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
			return true
		}
	}
	return false
}

// BundleOpts controls sugar vs bank-manual generation.
type BundleOpts struct {
	AWG3 bool
	// Mode: ""|"sugar" → id/ip/ib sugar (lx builds CPS at runtime).
	// "none"|"manual" → bank i1–i5 with selective entropy.
	Mode string
	// Preferred biases bank protocol family or sugar ip (quic|dns|stun|sip).
	Preferred string
	// PreserveID keeps an existing masquerade id (Reality SNI) on sugar regenerate.
	PreserveID string
}

// Bundle merges device (+ optional AWG3) into a flat map for hub.AWG.
// Default is sugar masquerade (ip/ib); id is left empty for the client SNI pool.
func Bundle(awg3 bool) (map[string]any, error) {
	return BundleWith(BundleOpts{AWG3: awg3, Mode: "sugar"})
}

// BundleManual generates junk + bank-based i1–i5 (no id/ip/ib).
func BundleManual(awg3 bool, preferred string) (map[string]any, error) {
	return BundleWith(BundleOpts{AWG3: awg3, Mode: "manual", Preferred: preferred})
}

// BundleWith generates an AWG hub map according to opts.
func BundleWith(opts BundleOpts) (map[string]any, error) {
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	preferred := strings.ToLower(strings.TrimSpace(opts.Preferred))
	switch mode {
	case "quic", "dns", "stun", "sip":
		if preferred == "" {
			preferred = mode
		}
		mode = "sugar"
	case "none", "manual":
		mode = "manual"
	case "", "sugar":
		mode = "sugar"
	default:
		mode = "sugar"
	}
	manual := mode == "manual"

	var out map[string]any
	if manual {
		proto, _, i1, i2, i3, i4, i5 := GenerateInitPacketsDetailed(preferred)
		dev := GenerateDeviceParamsForProtocol(opts.AWG3, proto)
		out = map[string]any{
			"jc": dev.JC, "jmin": dev.JMin, "jmax": dev.JMax,
			"s1": dev.S1, "s2": dev.S2, "s3": dev.S3, "s4": dev.S4,
			"h1": dev.H1, "h2": dev.H2, "h3": dev.H3, "h4": dev.H4,
			"i1": i1, "i2": i2, "i3": i3, "i4": i4, "i5": i5,
			"signature_protocol": proto,
		}
	} else {
		dev := GenerateDeviceParams(opts.AWG3)
		masq := GenerateMasquerade()
		ip := preferred
		if ip == "" || ip == "none" || ip == "manual" || ip == "sugar" {
			ip = masq.IP
		}
		switch ip {
		case "quic", "dns", "stun", "sip":
		default:
			ip = masq.IP
		}
		out = map[string]any{
			"jc": dev.JC, "jmin": dev.JMin, "jmax": dev.JMax,
			"s1": dev.S1, "s2": dev.S2, "s3": dev.S3, "s4": dev.S4,
			"h1": dev.H1, "h2": dev.H2, "h3": dev.H3, "h4": dev.H4,
			"ip": ip, "ib": masq.IB,
		}
		if id := strings.TrimSpace(opts.PreserveID); id != "" {
			out["id"] = id
		}
	}

	if opts.AWG3 {
		p3, err := GenerateAWG3Params()
		if err != nil {
			return nil, err
		}
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

// BundleFromExisting regenerates junk/CPS preserving sugar vs manual style of prev.
func BundleFromExisting(awg3 bool, prev map[string]any, modeOverride string) (map[string]any, error) {
	mode := strings.ToLower(strings.TrimSpace(modeOverride))
	preserveID := ""
	preferred := ""
	if mode == "" {
		if HasManualCPS(prev) {
			mode = "manual"
			if prev != nil {
				preferred = strings.TrimSpace(fmt.Sprint(prev["signature_protocol"]))
				if preferred == "<nil>" {
					preferred = ""
				}
			}
		} else {
			mode = "sugar"
			if prev != nil {
				preferred = strings.ToLower(strings.TrimSpace(fmt.Sprint(prev["ip"])))
				if preferred == "<nil>" {
					preferred = ""
				}
				preserveID = strings.TrimSpace(fmt.Sprint(prev["id"]))
				if preserveID == "<nil>" {
					preserveID = ""
				}
			}
		}
	}
	return BundleWith(BundleOpts{
		AWG3:       awg3,
		Mode:       mode,
		Preferred:  preferred,
		PreserveID: preserveID,
	})
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
