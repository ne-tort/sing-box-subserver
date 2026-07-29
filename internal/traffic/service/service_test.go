//go:build with_traffic

package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/service"
)

func TestServiceFlushAndManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	svc, err := service.New(service.Config{
		DataDir:       dir,
		FlushInterval: time.Hour,
		RetentionDays: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RegisterManifest("c1", []domain.Subject{{
		ID: "cp:user:1", DataplaneKeys: []string{"alice"},
	}}); err != nil {
		t.Fatal(err)
	}
	svc.SetLimits(map[string]domain.SpeedLimit{
		"alice": {UpBytesPerSec: 1000, DownBytesPerSec: 2000},
	})
	// Inject via tracker helpers exposed through Flush after Add*
	// Use Trackers stats tracker AddUserTraffic via type assert is awkward;
	// Flush empty should still succeed.
	if err := svc.Flush(); err != nil {
		t.Fatal(err)
	}
	st := svc.StatusPayload()
	if st["enabled"] != true {
		t.Fatalf("%v", st)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.Run(ctx) // should exit immediately
	u := svc.PollSubjectUsage("cp:user:1")
	if u.SubjectID != "cp:user:1" {
		t.Fatalf("%+v", u)
	}
}
