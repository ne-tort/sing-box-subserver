//go:build with_controlplane

package wgawg

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

// RandomizeCPS takes a bank CPS string (usually one <b 0x…> dump) and rewrites
// only ephemeral fields as engine tags (<r N>, <t>) while keeping protocol
// structure. Supported tags match amneziawg-go newObfChain: b, t, r, rc, rd, …
func RandomizeCPS(protocol, spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return ""
	}
	tags := parseCPSTags(spec)
	if len(tags) == 0 {
		return spec
	}
	// Mixed template already (fixtures): re-emit tags; rewrite nested <b> blobs.
	if len(tags) > 1 || tags[0].key != "b" {
		var b strings.Builder
		for _, t := range tags {
			b.WriteString(randomizeTag(protocol, t))
		}
		return b.String()
	}
	raw, err := decodeBTag(tags[0].val)
	if err != nil || len(raw) == 0 {
		return spec
	}
	return mutateBlob(protocol, raw)
}

type cpsTag struct {
	key string
	val string
}

func parseCPSTags(spec string) []cpsTag {
	var out []cpsTag
	remaining := spec
	for {
		start := strings.IndexByte(remaining, '<')
		if start < 0 {
			break
		}
		end := strings.IndexByte(remaining[start:], '>')
		if end < 0 {
			break
		}
		end += start
		parts := strings.Fields(remaining[start+1 : end])
		if len(parts) > 0 {
			t := cpsTag{key: parts[0]}
			if len(parts) > 1 {
				t.val = parts[1]
			}
			out = append(out, t)
		}
		remaining = remaining[end+1:]
	}
	return out
}

func decodeBTag(val string) ([]byte, error) {
	val = strings.TrimSpace(val)
	val = strings.TrimPrefix(val, "0x")
	val = strings.TrimPrefix(val, "0X")
	if val == "" {
		return nil, fmt.Errorf("empty hex")
	}
	return hex.DecodeString(val)
}

func firstBlob(spec string) ([]byte, bool) {
	for _, t := range parseCPSTags(spec) {
		if t.key != "b" {
			continue
		}
		raw, err := decodeBTag(t.val)
		if err != nil || len(raw) == 0 {
			return nil, false
		}
		return raw, true
	}
	return nil, false
}

func randomizeTag(protocol string, t cpsTag) string {
	switch t.key {
	case "b":
		raw, err := decodeBTag(t.val)
		if err != nil || len(raw) == 0 {
			if t.val == "" {
				return "<b>"
			}
			return "<b " + t.val + ">"
		}
		return mutateBlob(protocol, raw)
	case "t":
		return "<t>"
	case "r", "rc", "rd":
		if t.val == "" {
			return "<" + t.key + ">"
		}
		return fmt.Sprintf("<%s %s>", t.key, t.val)
	default:
		if t.val == "" {
			return "<" + t.key + ">"
		}
		return fmt.Sprintf("<%s %s>", t.key, t.val)
	}
}

func emitB(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return "<b 0x" + hex.EncodeToString(b) + ">"
}

func emitR(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("<r %d>", n)
}

func emitT() string { return "<t>" }

func mutateBlob(protocol string, raw []byte) string {
	proto := strings.ToLower(strings.TrimSpace(protocol))
	switch {
	case proto == "dns":
		return mutateDNS(raw)
	case strings.HasPrefix(proto, "stun") || proto == "webrtc":
		return mutateSTUN(raw)
	case proto == "ntp":
		return mutateNTP(raw)
	case proto == "quic" || proto == "quic_browser":
		return mutateQUICInitial(raw)
	case proto == "quic_tls_browser":
		// TLS-looking captures, not QUIC wire — keep dump, do not invent a parody.
		return emitB(raw)
	case strings.HasPrefix(proto, "sip"):
		return emitB(raw)
	case proto == "dtls":
		return mutateDTLS(raw)
	default:
		return emitB(raw)
	}
}

// DNS: only TXID (2B) is ephemeral; keep flags/question/OPT intact.
func mutateDNS(raw []byte) string {
	if len(raw) < 12 {
		return emitB(raw)
	}
	return emitR(2) + emitB(raw[2:])
}

// STUN: magic cookie fixed; 12-byte transaction ID is random; attributes stay.
func mutateSTUN(raw []byte) string {
	if len(raw) < 20 {
		return emitB(raw)
	}
	var b strings.Builder
	b.WriteString(emitB(raw[:8]))
	b.WriteString(emitR(12))
	if len(raw) > 20 {
		b.WriteString(emitB(raw[20:]))
	}
	return b.String()
}

// NTP (48B): refresh transmit timestamp (last 8B) as two <t> (4B each).
func mutateNTP(raw []byte) string {
	if len(raw) < 48 {
		return emitB(raw)
	}
	return emitB(raw[:40]) + emitT() + emitT()
}

// isQUICInitial reports a QUIC v1 long-header Initial-shaped datagram.
func isQUICInitial(raw []byte) bool {
	if len(raw) < 18 {
		return false
	}
	if raw[0]&0x80 == 0 {
		return false
	}
	if raw[1] != 0 || raw[2] != 0 || raw[3] != 0 || raw[4] != 1 {
		return false
	}
	_, _, _, ok := parseQUICLongHeader(raw)
	return ok
}

// parseQUICLongHeader returns cleartext header end offset, dcidLen, scidLen.
func parseQUICLongHeader(raw []byte) (headerEnd, dcidLen, scidLen int, ok bool) {
	if len(raw) < 7 || raw[0]&0x80 == 0 {
		return 0, 0, 0, false
	}
	dcidLen = int(raw[5])
	off := 6
	if dcidLen < 0 || off+dcidLen > len(raw) {
		return 0, 0, 0, false
	}
	off += dcidLen
	if off >= len(raw) {
		return 0, 0, 0, false
	}
	scidLen = int(raw[off])
	off++
	if scidLen < 0 || off+scidLen > len(raw) {
		return 0, 0, 0, false
	}
	off += scidLen
	tokenLen, n := readQUICVarint(raw[off:])
	if n == 0 || off+n+int(tokenLen) > len(raw) {
		return 0, 0, 0, false
	}
	off += n + int(tokenLen)
	_, n2 := readQUICVarint(raw[off:])
	if n2 == 0 || off+n2 > len(raw) {
		return 0, 0, 0, false
	}
	off += n2
	return off, dcidLen, scidLen, true
}

func readQUICVarint(b []byte) (val uint64, n int) {
	if len(b) == 0 {
		return 0, 0
	}
	switch b[0] >> 6 {
	case 0:
		return uint64(b[0] & 0x3f), 1
	case 1:
		if len(b) < 2 {
			return 0, 0
		}
		return uint64(b[0]&0x3f)<<8 | uint64(b[1]), 2
	case 2:
		if len(b) < 4 {
			return 0, 0
		}
		return uint64(b[0]&0x3f)<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3]), 4
	default:
		if len(b) < 8 {
			return 0, 0
		}
		return binary.BigEndian.Uint64(b) &^ (uint64(0xc0) << 56), 8
	}
}

// mutateQUICInitial keeps the full cleartext long header (flags, version,
// length fields, token) and only refreshes connection IDs + encrypted body.
// Short-header / broken bank variants are left as a single <b> (callers should
// filter them out of the pick set).
func mutateQUICInitial(raw []byte) string {
	headerEnd, dcidLen, scidLen, ok := parseQUICLongHeader(raw)
	if !ok || !isQUICInitial(raw) {
		return emitB(raw)
	}
	// Layout: [0:6]=flags+ver+dcid_len | dcid | scid_len | scid | token* | length | body
	var b strings.Builder
	b.WriteString(emitB(raw[:6]))
	b.WriteString(emitR(dcidLen))
	scidLenOff := 6 + dcidLen
	b.WriteString(emitB(raw[scidLenOff : scidLenOff+1])) // scid_len
	if scidLen > 0 {
		b.WriteString(emitR(scidLen))
	}
	afterCIDs := scidLenOff + 1 + scidLen
	// token_len varint + token + length varint — structural, keep verbatim.
	b.WriteString(emitB(raw[afterCIDs:headerEnd]))
	body := len(raw) - headerEnd
	if body > 0 {
		b.WriteString(emitR(body))
	}
	return b.String()
}

// DTLS: keep record + handshake headers (~25B), refresh body entropy.
func mutateDTLS(raw []byte) string {
	const keep = 25
	if len(raw) <= keep+8 {
		return emitB(raw)
	}
	// Prefer real DTLS record (content_type 20-24, version 0xfeff/0xfefd).
	if raw[0] < 20 || raw[0] > 24 {
		return emitB(raw)
	}
	return emitB(raw[:keep]) + emitR(len(raw)-keep)
}
