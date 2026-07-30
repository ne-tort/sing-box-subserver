//go:build with_traffic && with_controlplane

package cpbridge

import (
	"context"
	"log/slog"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane"
	cpdomain "github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
	"github.com/ne-tort/sing-box-subserver/internal/traffic"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
)

const consumerID = "controlplane"

// Bridge syncs controlplane users with the traffic module.
type Bridge struct {
	cp  *controlplane.Service
	mod *traffic.Module
	log *slog.Logger
}

// Attach wires the bridge into controlplane materialize hooks.
func Attach(cp *controlplane.Service, mod *traffic.Module, log *slog.Logger) *Bridge {
	if cp == nil || mod == nil {
		return nil
	}
	b := &Bridge{cp: cp, mod: mod, log: log}
	cp.SetTrafficHooks(b)
	return b
}

// OnMaterialize implements controlplane.TrafficHooks.
func (b *Bridge) OnMaterialize(users []cpdomain.User, sets []cpdomain.InboundSet) {
	if b == nil {
		return
	}
	subjects := make([]domain.Subject, 0, len(users))
	limits := make(map[string]domain.SpeedLimit)
	for _, u := range users {
		keys := DataplaneKeysForUser(u, sets)
		subjects = append(subjects, domain.Subject{
			ID:            "cp:user:" + u.ID,
			Kinds:         []domain.SubjectKind{domain.KindControlplaneUser, domain.KindDataplaneUser},
			DataplaneKeys: keys,
			Labels: map[string]string{
				"cp_name":  u.Name,
				"user_id":  u.ID,
				"consumer": consumerID,
			},
		})
		lim := domain.SpeedLimit{
			UpBytesPerSec:   u.SpeedUpBytesPerSec,
			DownBytesPerSec: u.SpeedDownBytesPerSec,
		}
		// Only shape eligible users; ineligible are kicked and omitted from config.
		if u.Eligible(time.Now().UTC()) && (lim.UpBytesPerSec > 0 || lim.DownBytesPerSec > 0) {
			for _, k := range keys {
				limits[k] = lim
			}
		}
	}
	if err := b.mod.RegisterManifest(consumerID, subjects); err != nil && b.log != nil {
		b.log.Warn("traffic cpbridge register manifest failed", "err", err)
	}
	b.mod.SetCPLimits(limits)
}

// DataplaneKeysForUser returns metadata.User keys materialize will emit for this user.
func DataplaneKeysForUser(u cpdomain.User, sets []cpdomain.InboundSet) []string {
	seen := map[string]struct{}{}
	var keys []string
	add := func(k string) {
		if k == "" {
			return
		}
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	add(u.Name)
	for _, set := range sets {
		for _, b := range set.EffectiveBindings() {
			p, err := presets.Get(b.Preset)
			if err != nil {
				continue
			}
			variants := cpdomain.UserVariantsForProtocol(p.Protocol, b, p.DefaultUserVariants)
			if len(variants) == 0 {
				continue
			}
			for _, vv := range variants {
				add(u.Name + "-" + vv.Name)
			}
		}
	}
	return keys
}

// Run polls usage into controlplane users until ctx cancels.
// Flush ownership stays with traffic.Service.Run — this loop only reads the store
// so GET /v1/traffic/onlines is not wiped by a second flush ticker.
func (b *Bridge) Run(ctx context.Context) {
	if b == nil {
		return
	}
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			// Final flush+sync so last live bytes reach users.json before exit.
			_ = b.mod.Flush()
			b.syncOnce(context.Background())
			return
		case <-t.C:
			b.syncOnce(ctx)
		}
	}
}

// SyncNow flushes traffic counters and pushes usage into controlplane (shutdown / tests).
func (b *Bridge) SyncNow(ctx context.Context) {
	if b == nil {
		return
	}
	_ = b.mod.Flush()
	b.syncOnce(ctx)
}

func (b *Bridge) syncOnce(ctx context.Context) {
	ids := b.cp.ListUserIDs()
	updates := make(map[string]uint64, len(ids))
	for _, id := range ids {
		u := b.mod.PollSubjectUsage("cp:user:" + id)
		if u.Total < 0 {
			continue
		}
		updates[id] = uint64(u.Total)
	}
	changed, err := b.cp.ApplyTrafficUsage(ctx, updates)
	if err != nil && b.log != nil {
		b.log.Warn("traffic cpbridge apply usage failed", "err", err)
		return
	}
	if changed && b.log != nil {
		b.log.Info("traffic cpbridge rematerialized after quota change")
	}
}

// OnTrafficReset is called by controlplane when a user's counter resets.
func (b *Bridge) OnTrafficReset(userID string) {
	if b == nil {
		return
	}
	_ = b.mod.ZeroSubject("cp:user:" + userID)
}

// OnTrafficUsedPatched syncs absolute admin-patched usage into the traffic store.
func (b *Bridge) OnTrafficUsedPatched(userID string, used uint64) {
	if b == nil {
		return
	}
	if err := b.mod.SetSubjectUsage("cp:user:"+userID, used); err != nil && b.log != nil {
		b.log.Warn("traffic cpbridge set usage failed", "user_id", userID, "err", err)
	}
}

// OnBecameIneligible kicks live sessions for dataplane keys of newly ineligible users.
func (b *Bridge) OnBecameIneligible(userIDs []string) {
	if b == nil || len(userIDs) == 0 {
		return
	}
	var keys []string
	seen := map[string]struct{}{}
	for _, id := range userIDs {
		for _, k := range b.dataplaneKeysForSubject("cp:user:" + id) {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	n := b.mod.CloseConnByUsers(keys)
	if n > 0 && b.log != nil {
		b.log.Info("traffic cpbridge kicked sessions for ineligible users", "users", len(userIDs), "conns", n)
	}
}

func (b *Bridge) dataplaneKeysForSubject(subjectID string) []string {
	svc := b.mod.Service()
	if svc == nil {
		return nil
	}
	for _, sub := range svc.Subjects() {
		if sub.ID == subjectID {
			return append([]string{}, sub.DataplaneKeys...)
		}
	}
	return nil
}
