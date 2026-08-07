//go:build with_controlplane

package controlplane

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/smoke"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/store"
)

func (s *Service) handleSmoke(w http.ResponseWriter, r *http.Request) {
	var body smoke.Request
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeBody(r, &body); err != nil {
			failJSON(w, 400, "bad_request", err.Error())
			return
		}
	}

	s.smokeMu.Lock()
	if s.smokeRunning {
		s.smokeMu.Unlock()
		failJSON(w, 409, "cp_smoke_busy", "smoke test already running")
		return
	}
	s.smokeRunning = true
	s.smokeMu.Unlock()
	defer func() {
		s.smokeMu.Lock()
		s.smokeRunning = false
		s.smokeMu.Unlock()
	}()

	sets, err := s.activeSetObjects()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	hub, err := s.store.LoadWgHub()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if len(sets) == 0 && !hub.Enabled {
		failJSON(w, 422, "cp_no_active_set", "no active sets")
		return
	}

	user, err := s.ensureSmokeProbeUser(r.Context())
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if hub.Enabled {
		ensured, credsChanged, err := s.ensureWgUserCreds([]domain.User{user})
		if err != nil {
			failJSON(w, 500, "internal", fmt.Sprintf("wg probe creds: %v", err))
			return
		}
		if len(ensured) == 1 {
			user = ensured[0]
		}
		if credsChanged {
			users, err := s.store.LoadUsers()
			if err != nil {
				failJSON(w, 500, "internal", err.Error())
				return
			}
			for i := range users {
				if users[i].ID == user.ID {
					users[i] = user
					break
				}
			}
			if err := s.store.SaveUsers(users); err != nil {
				failJSON(w, 500, "internal", err.Error())
				return
			}
			if err := s.rematerialize(r.Context()); err != nil {
				failJSON(w, 500, "internal", fmt.Sprintf("rematerialize after wg probe creds: %v", err))
				return
			}
			// Reload hub after rematerialize (keys may have been filled).
			hub, err = s.store.LoadWgHub()
			if err != nil {
				failJSON(w, 500, "internal", err.Error())
				return
			}
		}
	}
	_ = s.ensureSSLProfiles()
	sslList, err := s.loadSSLProfiles()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	defHost := s.publicHost(r)
	defCert, _ := sslCertPaths(s.cfg.DataDir, defaultSSLProfileID)
	for _, sp := range sslList {
		if sp.ID == defaultSSLProfileID {
			if sn := sp.ServerName(); sn != "" {
				defHost = sn
			}
			defCert, _ = sslCertPaths(s.cfg.DataDir, sp.ID)
			break
		}
	}
	profile := domain.DefaultSelfSigned(defHost)
	assignments, err := s.store.LoadRealityAssignments()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	sets, _, err = s.ensurePeerSecretsAll(sets)
	if err != nil {
		failJSON(w, 500, "internal", fmt.Sprintf("peer secrets: %v", err))
		return
	}
	var hubPtr *domain.WgHub
	if hub.Enabled {
		hubPtr = &hub
	}

	host := s.publicHost(r)
	in := smoke.Input{
		User:               user,
		Sets:               sets,
		PublicHost:         host,
		TLS:                profile,
		RealityAssignments: assignments,
		Hub:                hubPtr,
		TLSCertPath:        defCert,
		SSLProfiles:        s.sslProfilesWithResolvedACMEEmail(sslList),
	}
	report, err := smoke.Run(r.Context(), in, body)
	if err != nil {
		if strings.Contains(err.Error(), "cp_no_active_set") {
			failJSON(w, 422, "cp_no_active_set", err.Error())
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.store.SaveSmokeLast(report); err != nil {
		failJSON(w, 500, "internal", fmt.Sprintf("persist smoke last: %v", err))
		return
	}
	okJSON(w, 200, report)
}

func (s *Service) handleSmokeLast(w http.ResponseWriter, r *http.Request) {
	rep, err := s.store.LoadSmokeLast()
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			okJSON(w, 200, nil)
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, rep)
}

// ensureSmokeProbeUser picks credentials already present on live inbounds when possible.
// Prefer any enabled non-system user (no Apply). Otherwise ensure __cp_smoke__ and
// rematerialize so its UUID is accepted by active inbounds.
func (s *Service) ensureSmokeProbeUser(ctx context.Context) (domain.User, error) {
	users, err := s.store.LoadUsers()
	if err != nil {
		return domain.User{}, err
	}
	for i := range users {
		u := users[i]
		if !u.Enabled || smoke.IsSmokeUser(u.Name) {
			continue
		}
		changed, err := s.ensureCreds(&u)
		if err != nil {
			return domain.User{}, err
		}
		if changed {
			users[i] = u
			if err := s.store.SaveUsers(users); err != nil {
				return domain.User{}, err
			}
			// Creds changed for a live user → must rematerialize inbounds.
			if err := s.rematerialize(ctx); err != nil {
				return domain.User{}, fmt.Errorf("rematerialize after probe creds: %w", err)
			}
		}
		return u, nil
	}
	u, err := s.ensureSmokeUser()
	if err != nil {
		return domain.User{}, err
	}
	if err := s.rematerialize(ctx); err != nil {
		return domain.User{}, fmt.Errorf("rematerialize smoke user: %w", err)
	}
	return u, nil
}

func (s *Service) ensureSmokeUser() (domain.User, error) {
	users, err := s.store.LoadUsers()
	if err != nil {
		return domain.User{}, err
	}
	for i := range users {
		if smoke.IsSmokeUser(users[i].Name) {
			changed, err := s.ensureCreds(&users[i])
			if err != nil {
				return domain.User{}, err
			}
			if !users[i].Enabled {
				users[i].Enabled = true
				users[i].UpdatedAt = time.Now().UTC()
				changed = true
			}
			if changed {
				if err := s.store.SaveUsers(users); err != nil {
					return domain.User{}, err
				}
			}
			return users[i], nil
		}
	}
	id, err := randomToken()
	if err != nil {
		return domain.User{}, err
	}
	tok, err := randomToken()
	if err != nil {
		return domain.User{}, err
	}
	now := time.Now().UTC()
	u := domain.User{
		ID:        "smoke-" + id[:8],
		Name:      smoke.SmokeUserName,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
		SubToken:  tok,
		Creds:     map[string]map[string]any{},
	}
	if _, err := s.ensureCreds(&u); err != nil {
		return domain.User{}, fmt.Errorf("smoke creds: %w", err)
	}
	users = append(users, u)
	if err := s.store.SaveUsers(users); err != nil {
		return domain.User{}, err
	}
	return u, nil
}
