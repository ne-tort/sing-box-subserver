//go:build with_controlplane

package smoke

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/box"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/materialize"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

type probeTarget struct {
	Outbound map[string]any
	Result   Result
}

type setPreset struct {
	Set    string
	Preset string
}

// Run builds subscription outbounds, starts an ephemeral client box, and probes each target.
func Run(ctx context.Context, in Input, req Request) (*Report, error) {
	start := time.Now()
	if ctx == nil {
		ctx = context.Background()
	}
	sets := filterSets(in.Sets, req.Sets, req.Presets)
	hubEnabled := in.Hub != nil && in.Hub.Enabled
	includeWg := hubEnabled && wgSmokeRequested(req)
	if len(sets) == 0 && !includeWg {
		return nil, fmt.Errorf("cp_no_active_set: no matching active sets")
	}

	var results []Result
	testable := make([]setPreset, 0)
	for _, set := range sets {
		for _, b := range set.EffectiveBindings() {
			pn := b.Preset
			if len(req.Presets) > 0 && !presetMatchesFilter(req.Presets, pn) {
				continue
			}
			if reason := SkipReason(pn); reason != "" {
				results = append(results, Result{
					Set: set.Name, Preset: pn,
					InboundTag: InboundTagFor(set.Name, pn),
					Skipped:    true, SkipReason: reason,
				})
				continue
			}
			testable = append(testable, setPreset{Set: set.Name, Preset: pn})
		}
	}

	filters := materialize.SubscriptionFilters{
		Presets:     req.Presets,
		TLSCertPath: in.TLSCertPath,
		SlotTLS:     in.SlotTLS,
	}
	if len(req.Sets) == 1 {
		filters.Set = req.Sets[0]
	}

	subBody, err := materialize.RenderSubscription(
		in.User, sets, in.PublicHost, in.TLS, in.CertManager, filters, in.RealityAssignments, in.Hub,
	)
	if err != nil {
		return nil, fmt.Errorf("render subscription: %w", err)
	}
	outbounds, err := ExtractOutbounds(subBody)
	if err != nil {
		return nil, err
	}
	outbounds, err = CloneOutbounds(outbounds)
	if err != nil {
		return nil, err
	}
	if err := RewriteServersToHairpin(outbounds); err != nil {
		return nil, err
	}

	targets := make([]probeTarget, 0, len(outbounds)+1)
	bestPrimary := map[string]probeTarget{}
	bestScore := map[string]int{}
	for _, raw := range outbounds {
		ob, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		tag := outboundTag(ob)
		sp, ok := matchSetPreset(tag, testable)
		if !ok {
			continue
		}
		variant, profile := variantProfileFromTag(tag, sp.Preset)
		pt := probeTarget{
			Outbound: ob,
			Result: Result{
				Set: sp.Set, Preset: sp.Preset,
				InboundTag:  InboundTagFor(sp.Set, sp.Preset),
				OutboundTag: tag,
				Variant:     variant,
				Profile:     profile,
			},
		}
		if req.IncludeVariants {
			targets = append(targets, pt)
			continue
		}
		key := sp.Set + "/" + sp.Preset
		score := primaryOutboundScore(sp.Preset, variant, profile)
		if prev, ok := bestScore[key]; ok && score <= prev {
			continue
		}
		bestScore[key] = score
		bestPrimary[key] = pt
	}
	if !req.IncludeVariants {
		for _, pt := range bestPrimary {
			targets = append(targets, pt)
		}
	}

	if includeWg {
		eps, err := ExtractEndpoints(subBody)
		if err != nil {
			return nil, err
		}
		eps, err = CloneOutbounds(eps)
		if err != nil {
			return nil, err
		}
		if err := RewriteServersToHairpin(eps); err != nil {
			return nil, err
		}
		var wgOb map[string]any
		for _, raw := range eps {
			ob, ok := raw.(map[string]any)
			if !ok || !isWireGuardType(ob) {
				continue
			}
			tag := outboundTag(ob)
			if tag == WgSmokeTag || strings.HasPrefix(tag, "cp-wg") {
				wgOb = ob
				break
			}
			if wgOb == nil {
				wgOb = ob
			}
		}
		if wgOb == nil {
			results = append(results, Result{
				Set: WgSmokeSetName, Preset: WgSmokePreset,
				InboundTag: "cp-wg",
				Skipped:    true, SkipReason: "wg_no_client_endpoint",
			})
		} else {
			tag := outboundTag(wgOb)
			if tag == "" || tag == "<nil>" {
				tag = WgSmokeTag
				wgOb["tag"] = tag
			}
			targets = append(targets, probeTarget{
				Outbound: wgOb,
				Result: Result{
					Set:         WgSmokeSetName,
					Preset:      WgSmokePreset,
					InboundTag:  "cp-wg",
					OutboundTag: tag,
				},
			})
		}
	}

	// Drop outbounds that cannot start (e.g. DERP with empty peer_public_key) so one
	// bad preset does not abort the whole ephemeral smoke box.
	usable := make([]probeTarget, 0, len(targets))
	for _, t := range targets {
		if reason := outboundStartBlockReason(t.Outbound); reason != "" {
			r := t.Result
			r.OK = false
			r.Error = reason
			results = append(results, r)
			continue
		}
		usable = append(usable, t)
	}
	targets = usable

	if len(targets) == 0 {
		return &Report{DurationMs: time.Since(start).Milliseconds(), Results: results}, nil
	}

	ports := make([]int, len(targets))
	for i := range targets {
		p, err := freeTCPPort()
		if err != nil {
			return nil, err
		}
		ports[i] = p
	}

	cfg, err := buildEphemeralConfig(targets, ports)
	if err != nil {
		return nil, err
	}
	eng := box.NewEngine(context.Background())
	inst, err := eng.Start(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("ephemeral box: %w", err)
	}
	defer func() { _ = inst.Close() }()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(boxSettle):
	}

	timeout := req.EffectiveTimeout()
	urls := req.EffectiveURLs()
	sem := make(chan struct{}, defaultParallel)
	var wg sync.WaitGroup
	var mu sync.Mutex
	probeResults := make([]Result, len(targets))

	for i, t := range targets {
		i, t := i, t
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r := t.Result
			socks := fmt.Sprintf("%s:%d", HairpinLocalHost, ports[i])
			pctx, cancel := context.WithTimeout(ctx, timeout*time.Duration(len(urls)+1))
			defer cancel()
			ok, ms, used, perr := ProbeViaSOCKS(pctx, socks, urls, timeout)
			if ok {
				r.OK = true
				r.LatencyMs = &ms
				r.URL = used
			} else {
				r.OK = false
				if perr != nil {
					r.Error = perr.Error()
				} else {
					r.Error = "probe failed"
				}
			}
			mu.Lock()
			probeResults[i] = r
			mu.Unlock()
		}()
	}
	wg.Wait()
	results = append(results, probeResults...)

	return &Report{
		DurationMs: time.Since(start).Milliseconds(),
		Results:    results,
	}, nil
}

func wgSmokeRequested(req Request) bool {
	if len(req.Presets) > 0 && !presetMatchesFilter(req.Presets, WgSmokePreset) {
		return false
	}
	if len(req.Sets) == 0 {
		return true
	}
	return strIn(req.Sets, WgSmokeSetName)
}

func freeTCPPort() (int, error) {
	ln, err := net.Listen("tcp", HairpinLocalHost+":0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// primaryOutboundScore ranks outbounds for include_variants=false.
// Prefer preset default_user_variants / default_client_profiles over lexicographic first.
func primaryOutboundScore(preset, variant, profile string) int {
	score := 0
	p, err := presets.Get(preset)
	if err != nil {
		// Prefer plain flow-none-ish tags when catalog lookup fails.
		if variant == "none" || variant == "" {
			score += 10
		}
		if strings.HasPrefix(profile, "pkt-xudp") || profile == "" {
			score += 5
		}
		return score
	}
	defV := p.DefaultUserVariants
	if len(defV) == 0 {
		defV = []string{"flow-none"}
	}
	wantVariant := strings.TrimPrefix(defV[0], "flow-")
	if variant == wantVariant || (wantVariant == "none" && (variant == "none" || variant == "")) {
		score += 100
	} else if variant == "none" || variant == "" {
		score += 20
	}
	defP := p.DefaultClientProfiles
	if len(defP) == 0 {
		defP = []string{"pkt-xudp"}
	}
	if profile == defP[0] {
		score += 50
	} else if profile == "" && defP[0] == "pkt-xudp" {
		// Some tags omit profile when only one default exists.
		score += 25
	}
	return score
}

func buildEphemeralConfig(targets []probeTarget, ports []int) ([]byte, error) {
	inbounds := make([]any, 0, len(targets))
	outbounds := make([]any, 0, len(targets)+1)
	endpoints := make([]any, 0)
	outbounds = append(outbounds, map[string]any{"type": "direct", "tag": "direct"})
	rules := make([]any, 0, len(targets))

	for i, t := range targets {
		inTag := fmt.Sprintf("smoke-in-%d", i)
		obTag := t.Result.OutboundTag
		inbounds = append(inbounds, map[string]any{
			"type":        "mixed",
			"tag":         inTag,
			"listen":      HairpinLocalHost,
			"listen_port": ports[i],
		})
		ob := t.Outbound
		ob["tag"] = obTag
		// WireGuard is an endpoint in sing-box-lx; dialer tag still works in route.
		if isWireGuardType(ob) {
			endpoints = append(endpoints, ob)
		} else {
			outbounds = append(outbounds, ob)
		}
		rules = append(rules, map[string]any{
			"inbound":  []any{inTag},
			"outbound": obTag,
		})
	}

	doc := map[string]any{
		"log":       map[string]any{"level": "warn"},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"route": map[string]any{
			"rules": rules,
			"final": "direct",
		},
	}
	if len(endpoints) > 0 {
		doc["endpoints"] = endpoints
	}
	return json.Marshal(doc)
}

// outboundStartBlockReason returns a non-empty reason when the outbound would fail
// box start (seen as: ephemeral box: initialize outbound[N]: peer_public_key: empty key).
func outboundStartBlockReason(ob map[string]any) string {
	if ob == nil {
		return "empty outbound"
	}
	typ, _ := ob["type"].(string)
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "derp":
		pk, _ := ob["peer_public_key"].(string)
		if strings.TrimSpace(pk) == "" {
			return "peer_public_key: empty key"
		}
		priv, _ := ob["private_key"].(string)
		if strings.TrimSpace(priv) == "" {
			return "private_key: empty key"
		}
	case "wireguard":
		peers, _ := ob["peers"].([]any)
		if len(peers) == 0 {
			if pk, _ := ob["peer_public_key"].(string); strings.TrimSpace(pk) == "" {
				return "peer_public_key: empty key"
			}
		}
		priv, _ := ob["private_key"].(string)
		if strings.TrimSpace(priv) == "" {
			return "private_key: empty key"
		}
	}
	return ""
}

func filterSets(all []domain.InboundSet, setFilter, presetFilter []string) []domain.InboundSet {
	out := make([]domain.InboundSet, 0, len(all))
	for _, set := range all {
		if len(setFilter) > 0 && !strIn(setFilter, set.Name) {
			continue
		}
		if len(presetFilter) == 0 {
			out = append(out, set)
			continue
		}
		// Keep set if any binding matches preset filter.
		cp := set
		var bindings []domain.SetBinding
		for _, b := range set.EffectiveBindings() {
			if presetMatchesFilter(presetFilter, b.Preset) {
				bindings = append(bindings, b)
			}
		}
		if len(bindings) == 0 {
			continue
		}
		cp.Bindings = bindings
		cp.Presets = nil
		out = append(out, cp)
	}
	return out
}

func matchSetPreset(tag string, testable []setPreset) (setPreset, bool) {
	// Prefer longest preset match: cp-out-{set}-{preset}...
	const prefix = "cp-out-"
	if !strings.HasPrefix(tag, prefix) {
		return setPreset{}, false
	}
	rest := strings.TrimPrefix(tag, prefix)
	var best setPreset
	bestLen := -1
	for _, sp := range testable {
		want := sp.Set + "-" + sp.Preset
		if rest == want || strings.HasPrefix(rest, want+"-") {
			if len(want) > bestLen {
				best = sp
				bestLen = len(want)
			}
		}
		// Alias forms: preset may be alias in binding but tag uses canonical or alias.
		if p, err := presets.Get(sp.Preset); err == nil {
			for _, a := range append([]string{p.Name}, p.Aliases...) {
				want2 := sp.Set + "-" + a
				if rest == want2 || strings.HasPrefix(rest, want2+"-") {
					if len(want2) > bestLen {
						best = setPreset{Set: sp.Set, Preset: sp.Preset}
						bestLen = len(want2)
					}
				}
			}
		}
	}
	if bestLen < 0 {
		return setPreset{}, false
	}
	return best, true
}

func variantProfileFromTag(tag, preset string) (variant, profile string) {
	// cp-out-{set}-{preset}-{variantSuffix}[-{profile}]
	// variantSuffix is flow name without "flow-" prefix (none, xtls-rprx-vision, udp-vision).
	const prefix = "cp-out-"
	rest := strings.TrimPrefix(tag, prefix)
	// Find preset occurrence.
	idx := strings.Index(rest, "-"+preset)
	if idx < 0 {
		// try after first dash segment (set)
		parts := strings.SplitN(rest, "-", 2)
		if len(parts) < 2 {
			return "", ""
		}
		rest = parts[1]
		if strings.HasPrefix(rest, preset) {
			rest = strings.TrimPrefix(rest, preset)
			rest = strings.TrimPrefix(rest, "-")
		}
	} else {
		rest = rest[idx+1+len(preset):]
		rest = strings.TrimPrefix(rest, "-")
	}
	if rest == "" {
		return "", ""
	}
	parts := strings.Split(rest, "-")
	// profiles are pkt-none, pkt-xudp, pkt-packetaddr, sec-*, udp-*
	if len(parts) >= 2 && (parts[len(parts)-2] == "pkt" || parts[len(parts)-2] == "sec" || parts[len(parts)-2] == "udp") {
		profile = parts[len(parts)-2] + "-" + parts[len(parts)-1]
		variant = strings.Join(parts[:len(parts)-2], "-")
		return variant, profile
	}
	// single trailing profile like pkt-xudp when joined split wrong — check full rest
	if strings.HasPrefix(rest, "pkt-") || strings.HasPrefix(rest, "sec-") {
		return "", rest
	}
	return rest, ""
}

func presetMatchesFilter(filter []string, preset string) bool {
	for _, f := range filter {
		if f == preset {
			return true
		}
		if p, err := presets.Get(f); err == nil && (p.Name == preset || preset == f) {
			return true
		}
		if p, err := presets.Get(preset); err == nil {
			if p.Name == f {
				return true
			}
			for _, a := range p.Aliases {
				if a == f {
					return true
				}
			}
		}
	}
	return false
}

func strIn(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
