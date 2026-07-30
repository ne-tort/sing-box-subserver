//go:build with_controlplane

package controlplane

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

const maxOwnerTransitions = 20

func (s *Service) recordMaterializeResult(success bool, err error, noop bool, sha string, code string) {
	if s == nil {
		return
	}
	st, loadErr := s.store.LoadState()
	if loadErr != nil {
		return
	}
	now := time.Now().UTC()
	ms := st.Materialize
	if ms == nil {
		ms = &domain.MaterializeStatus{}
	}
	ms.LastAttemptAt = &now
	ms.LastApplyNoop = noop
	if success {
		ms.LastSuccessAt = &now
		ms.LastError = ""
		ms.LastErrorCode = ""
		st.LastMaterializeAt = &now
		if sha != "" {
			st.LastMaterializeSHA256 = sha
		}
	} else {
		if err != nil {
			ms.LastError = err.Error()
		}
		ms.LastErrorCode = code
	}
	st.Materialize = ms
	_ = s.store.SaveState(st)
}

func (s *Service) recordOwnerTransition(from, to configowner.Mode, reason, trigger string, success bool, err error) {
	if s == nil {
		return
	}
	st, loadErr := s.store.LoadState()
	if loadErr != nil {
		return
	}
	entry := domain.OwnerTransition{
		From:    string(from),
		To:      string(to),
		At:      time.Now().UTC(),
		Reason:  reason,
		Trigger: trigger,
		Success: success,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	st.OwnerTransitions = append(st.OwnerTransitions, entry)
	if len(st.OwnerTransitions) > maxOwnerTransitions {
		st.OwnerTransitions = st.OwnerTransitions[len(st.OwnerTransitions)-maxOwnerTransitions:]
	}
	_ = s.store.SaveState(st)
}

func (s *Service) claimOwnership(mode configowner.Mode, reason, trigger string) error {
	if s == nil || s.cfg.Owner == nil {
		return fmt.Errorf("configowner unavailable")
	}
	from := s.cfg.Owner.Owner()
	if from == mode {
		return nil
	}
	err := s.cfg.Owner.Claim(mode)
	s.recordOwnerTransition(from, mode, reason, trigger, err == nil, err)
	return err
}

func (s *Service) reconcileOwnershipOnBoot(ctx context.Context) {
	if s == nil || s.cfg.Owner == nil {
		return
	}
	s.scrubStaleActiveSets()
	st, err := s.store.LoadState()
	if err != nil {
		return
	}
	mode := s.cfg.Owner.Owner()
	if mode != configowner.ModeControlplane && len(st.ActiveSets) > 0 {
		if s.log != nil {
			s.log.Warn("controlplane boot: clearing stale active_sets", "mode", mode, "active_sets", st.ActiveSets)
		}
		st.ActiveSets = nil
		_ = s.store.SaveState(st)
		return
	}
	live := s.cpDataplaneLive(st)
	if mode == configowner.ModeControlplane && !live {
		if err := s.claimOwnership(configowner.ModeIdle, "boot_reconcile_orphan", ""); err != nil && s.log != nil {
			s.log.Warn("controlplane boot orphan ownership rollback failed", "err", err)
		} else if s.log != nil {
			s.log.Warn("controlplane boot: rolled back orphan controlplane ownership")
		}
		return
	}
	if mode == configowner.ModeControlplane && live {
		if err := s.rematerialize(ctx); err != nil && s.log != nil {
			s.log.Warn("controlplane boot rematerialize failed", "err", err)
		}
	}
}

// cpDataplaneLive reports whether controlplane owns live dataplane work
// (active inbound sets and/or enabled WireGuard hub).
func (s *Service) cpDataplaneLive(st domain.State) bool {
	if len(st.ActiveSets) > 0 {
		return true
	}
	if s == nil || s.store == nil {
		return false
	}
	hub, err := s.store.LoadWgHub()
	return err == nil && hub.Enabled
}

func (s *Service) ownershipHealth(st domain.State) map[string]any {
	mode := configowner.ModeIdle
	if s != nil && s.cfg.Owner != nil {
		mode = s.cfg.Owner.Owner()
	}
	live := s.cpDataplaneLive(st)
	health := "ok"
	issues := make([]string, 0)
	if mode == configowner.ModeControlplane && !live {
		health = "degraded"
		issues = append(issues, "controlplane_mode_without_active_sets")
	}
	if mode != configowner.ModeControlplane && len(st.ActiveSets) > 0 {
		health = "degraded"
		issues = append(issues, "active_sets_without_controlplane_ownership")
	}
	if st.Materialize != nil && st.Materialize.LastErrorCode != "" {
		health = "degraded"
		issues = append(issues, "last_materialize_failed")
	}
	if mode == configowner.ModeControlplane && live && st.LastMaterializeAt == nil {
		health = "degraded"
		issues = append(issues, "never_materialized")
	}
	return map[string]any{
		"status": health,
		"issues": issues,
	}
}

func materializeErrorCode(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "validate") || strings.Contains(msg, "template") || strings.Contains(msg, "missing") {
		return "cp_materialize_failed"
	}
	return "cp_apply_failed"
}

// activateErrorCode maps activateSetByName failures for from-* when activate:true → HTTP 422.
func activateErrorCode(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "cp_claim_failed") || strings.Contains(msg, "claim"):
		return "cp_claim_failed"
	case strings.Contains(msg, "unsupported_build_tag") || strings.Contains(msg, "with_demux"):
		return "unsupported_build_tag"
	default:
		return materializeErrorCode(err)
	}
}
