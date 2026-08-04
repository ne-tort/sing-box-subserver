//go:build with_controlplane

package wgawg

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed data/signatures.seed.json
var signaturesSeedJSON []byte

//go:embed data/junk-ranges.seed.json
var junkRangesSeedJSON []byte

const (
	slotI1 = "i1"
	slotI2 = "i2"
	slotI3 = "i3"
	slotI4 = "i4"
	slotI5 = "i5"
)

var slotKeys = []string{slotI1, slotI2, slotI3, slotI4, slotI5}

// SignatureSlots is one bank variant (i1–i5 CPS strings).
type SignatureSlots struct {
	I1 string `json:"i1"`
	I2 string `json:"i2"`
	I3 string `json:"i3"`
	I4 string `json:"i4"`
	I5 string `json:"i5"`
}

type signaturesBank struct {
	Version  int                                   `json:"version"`
	Target   int                                   `json:"target"`
	Profiles map[string]map[string]SignatureSlots `json:"profiles"`
}

type junkRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type junkProtocolRanges struct {
	JC   *junkRange `json:"jc"`
	JMin *junkRange `json:"jmin"`
	JMax *junkRange `json:"jmax"`
	S1   *junkRange `json:"s1"`
	S2   *junkRange `json:"s2"`
	S3   *junkRange `json:"s3"`
	S4   *junkRange `json:"s4"`
}

type junkRangesBank struct {
	Version   int                            `json:"version"`
	Defaults  junkProtocolRanges             `json:"defaults"`
	Protocols map[string]junkProtocolRanges `json:"protocols"`
}

var (
	bankOnce sync.Once
	bank     *signaturesBank
	bankErr  error

	junkOnce sync.Once
	junkBank *junkRangesBank
	junkErr  error
)

func loadSignaturesBank() (*signaturesBank, error) {
	bankOnce.Do(func() {
		var b signaturesBank
		if err := json.Unmarshal(signaturesSeedJSON, &b); err != nil {
			bankErr = fmt.Errorf("signatures.seed.json: %w", err)
			return
		}
		if len(b.Profiles) == 0 {
			bankErr = fmt.Errorf("signatures.seed.json: empty profiles")
			return
		}
		bank = &b
	})
	return bank, bankErr
}

func loadJunkRanges() (*junkRangesBank, error) {
	junkOnce.Do(func() {
		var b junkRangesBank
		if err := json.Unmarshal(junkRangesSeedJSON, &b); err != nil {
			junkErr = fmt.Errorf("junk-ranges.seed.json: %w", err)
			return
		}
		junkBank = &b
	})
	return junkBank, junkErr
}

func listProtocols(b *signaturesBank) []string {
	out := make([]string, 0, len(b.Profiles))
	for pid := range b.Profiles {
		if len(listVariants(pid, b.Profiles[pid])) == 0 {
			continue
		}
		out = append(out, pid)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if protocolLess(out[j], out[i]) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func protocolLess(a, b string) bool {
	if a == "dns" {
		return true
	}
	if b == "dns" {
		return false
	}
	return a < b
}

// variantUsable rejects broken bank entries (e.g. quic v2–v10 with corrupted
// long-header / version that would otherwise become <b 0x3e><r 1249>).
func variantUsable(protocol string, slots SignatureSlots) bool {
	i1 := strings.TrimSpace(slots.I1)
	if i1 == "" {
		return false
	}
	proto := strings.ToLower(strings.TrimSpace(protocol))
	switch {
	case proto == "quic_tls_browser":
		return false
	case proto == "quic" || proto == "quic_browser":
		raw, ok := firstBlob(i1)
		return ok && isQUICInitial(raw)
	default:
		return true
	}
}

func listVariants(protocol string, variants map[string]SignatureSlots) []string {
	nums := make([]int, 0, len(variants))
	for k, slots := range variants {
		n := 0
		if _, err := fmt.Sscanf(k, "%d", &n); err != nil || n < 1 {
			continue
		}
		if !variantUsable(protocol, slots) {
			continue
		}
		nums = append(nums, n)
	}
	for i := 0; i < len(nums); i++ {
		for j := i + 1; j < len(nums); j++ {
			if nums[j] < nums[i] {
				nums[i], nums[j] = nums[j], nums[i]
			}
		}
	}
	out := make([]string, len(nums))
	for i, n := range nums {
		out[i] = fmt.Sprintf("%d", n)
	}
	return out
}

// PickSignature picks a random protocol+variant from the bank.
// preferred maps masquerade sugar (quic|dns|stun|sip) onto bank protocol families.
func PickSignature(preferred string) (protocol string, variant string, slots SignatureSlots, err error) {
	b, err := loadSignaturesBank()
	if err != nil {
		return "", "", SignatureSlots{}, err
	}
	preferred = strings.ToLower(strings.TrimSpace(preferred))
	candidates := protocolsForPreferred(preferred, b)
	if len(candidates) == 0 {
		candidates = listProtocols(b)
	}
	if len(candidates) == 0 {
		return "", "", SignatureSlots{}, fmt.Errorf("signatures bank: no protocols")
	}
	protocol = candidates[rnd(0, len(candidates)-1)]
	variants := listVariants(protocol, b.Profiles[protocol])
	if len(variants) == 0 {
		return "", "", SignatureSlots{}, fmt.Errorf("signatures bank: no variants for %s", protocol)
	}
	variant = variants[rnd(0, len(variants)-1)]
	slots = b.Profiles[protocol][variant]
	return protocol, variant, slots, nil
}

func protocolsForPreferred(preferred string, b *signaturesBank) []string {
	all := listProtocols(b)
	if preferred == "" || preferred == "none" {
		return all
	}
	var want []string
	switch preferred {
	case "quic":
		want = []string{"quic_browser", "quic"}
	case "dns":
		want = []string{"dns"}
	case "stun":
		want = []string{"stun_browser", "stun", "webrtc"}
	case "sip":
		want = []string{"sip_multi", "sip"}
	default:
		want = []string{preferred}
	}
	out := make([]string, 0, len(want))
	set := map[string]struct{}{}
	for _, p := range all {
		set[p] = struct{}{}
	}
	for _, p := range want {
		if _, ok := set[p]; ok {
			out = append(out, p)
		}
	}
	return out
}

func mergeJunk(base, over junkProtocolRanges) junkProtocolRanges {
	out := base
	if over.JC != nil {
		out.JC = over.JC
	}
	if over.JMin != nil {
		out.JMin = over.JMin
	}
	if over.JMax != nil {
		out.JMax = over.JMax
	}
	if over.S1 != nil {
		out.S1 = over.S1
	}
	if over.S2 != nil {
		out.S2 = over.S2
	}
	if over.S3 != nil {
		out.S3 = over.S3
	}
	if over.S4 != nil {
		out.S4 = over.S4
	}
	return out
}

func rangesForProtocol(protocol string) junkProtocolRanges {
	b, err := loadJunkRanges()
	if err != nil || b == nil {
		return junkProtocolRanges{}
	}
	out := b.Defaults
	if over, ok := b.Protocols[protocol]; ok {
		out = mergeJunk(out, over)
	}
	return out
}

func pickIn(r *junkRange, fallbackLo, fallbackHi int) int {
	if r == nil || r.Max < r.Min || r.Max <= 0 {
		return rnd(fallbackLo, fallbackHi)
	}
	return rnd(r.Min, r.Max)
}

// GenerateDeviceParamsForProtocol calibrates Jc/S* to the signature protocol cluster
// (amnezia-wg-easy junk-ranges.seed.json).
func GenerateDeviceParamsForProtocol(awg3 bool, protocol string) DeviceParams {
	dev := GenerateDeviceParams(awg3)
	r := rangesForProtocol(protocol)
	if r.JC == nil && r.JMin == nil && r.JMax == nil {
		return dev
	}
	sMin := 1
	if awg3 {
		sMin = 12
	}
	dev.JC = pickIn(r.JC, 3, 10)
	dev.JMin = pickIn(r.JMin, 128, 512)
	dev.JMax = pickIn(r.JMax, 512, 1024)
	if dev.JMax <= dev.JMin+64 {
		dev.JMax = dev.JMin + 64 + rnd(64, 256)
	}
	if r.S1 != nil {
		dev.S1 = maxInt(sMin, pickIn(r.S1, sMin, 150))
	}
	if r.S2 != nil {
		dev.S2 = maxInt(sMin, pickIn(r.S2, sMin, 150))
		for attempts := 0; attempts < 8 && dev.S2 == dev.S1+56; attempts++ {
			dev.S2 = maxInt(sMin, pickIn(r.S2, sMin, 150))
		}
	}
	if r.S3 != nil {
		dev.S3 = maxInt(sMin, pickIn(r.S3, sMin, 64))
	}
	if r.S4 != nil {
		dev.S4 = maxInt(sMin, pickIn(r.S4, sMin, 32))
	}
	return dev
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
