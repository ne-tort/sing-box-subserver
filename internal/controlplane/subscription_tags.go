//go:build with_controlplane

package controlplane

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

func (s *Service) bindingSubscriptionTagsEntry(set domain.InboundSet, b domain.SetBinding) map[string]any {
	p, err := presets.Get(b.Preset)
	if err != nil {
		return nil
	}
	variantNames := []string{}
	queryTags := []string{}
	for _, vv := range domain.UserVariantsForProtocol(p.Protocol, b, p.DefaultUserVariants) {
		variantNames = append(variantNames, vv.Name)
		queryTags = append(queryTags, vv.QueryTags...)
	}
	profileNames := []string{}
	for _, cp := range domain.ClientProfilesForProtocol(p.Protocol, b, p.DefaultClientProfiles) {
		profileNames = append(profileNames, cp.Name)
		queryTags = append(queryTags, cp.QueryTags...)
	}
	queryTags = append(queryTags, b.SubscriptionTags...)
	enabledProfiles := dedupeStrings(b.EnabledClientProfiles)
	if len(enabledProfiles) == 0 {
		enabledProfiles = dedupeStrings(profileNames)
	}
	return map[string]any{
		"inbound_tag":                fmt.Sprintf("cp-in-%s-%s", set.Name, b.Preset),
		"preset":                     b.Preset,
		"protocol":                   p.Protocol,
		"subscription_tags":          dedupeStrings(queryTags),
		"enabled_user_variants":      dedupeStrings(variantNames),
		"enabled_client_profiles":    enabledProfiles,
		"credential_instance_policy": b.CredentialInstancePolicy,
		"params":                     b.Params,
		"param_fields":               append([]string{}, p.ParamFields...),
	}
}

func (s *Service) buildSetSubscriptionTagsPayload(set domain.InboundSet) map[string]any {
	bindings := set.EffectiveBindings()
	out := make([]any, 0, len(bindings))
	for _, b := range bindings {
		if entry := s.bindingSubscriptionTagsEntry(set, b); entry != nil {
			out = append(out, entry)
		}
	}
	return map[string]any{
		"set":      set.Name,
		"bindings": out,
	}
}

func (s *Service) handleSubscriptionTags(w http.ResponseWriter, r *http.Request) {
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	st, err := s.store.LoadState()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	activeOnly := parseBoolQuery(r, "active_only", true)
	names := append([]string{}, st.ActiveSets...)
	sort.Strings(names)
	active := map[string]struct{}{}
	for _, n := range names {
		active[n] = struct{}{}
	}
	byName := map[string]domain.InboundSet{}
	for _, set := range sets {
		byName[set.Name] = set
	}
	out := make([]any, 0)
	if activeOnly {
		for _, name := range names {
			set, ok := byName[name]
			if !ok {
				continue
			}
			payload := s.buildSetSubscriptionTagsPayload(set)
			payload["active"] = true
			out = append(out, payload)
		}
	} else {
		sort.Slice(sets, func(i, j int) bool { return sets[i].Name < sets[j].Name })
		for _, set := range sets {
			payload := s.buildSetSubscriptionTagsPayload(set)
			_, payload["active"] = active[set.Name]
			out = append(out, payload)
		}
	}
	okJSON(w, 200, map[string]any{"sets": out})
}

func parseBoolQuery(r *http.Request, key string, defaultVal bool) bool {
	v := r.URL.Query().Get(key)
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}
	return b
}
