//go:build with_traffic

package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/store"
)

func TestStoreRoundtripAndRetention(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.New(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSubjects("c1", []domain.Subject{{
		ID: "cp:user:1", DataplaneKeys: []string{"alice", "alice-flow-none"},
		Labels: map[string]string{"consumer": "c1"},
	}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := st.ApplySamples([]domain.Sample{
		{At: now, SeriesType: domain.SeriesDataplaneUser, Key: "alice", Up: 10, Down: 20},
		{At: now, SeriesType: domain.SeriesDataplaneUser, Key: "alice-flow-none", Up: 5, Down: 5},
		{At: now, SeriesType: domain.SeriesSubject, Key: "cp:user:1", Up: 15, Down: 25},
	}); err != nil {
		t.Fatal(err)
	}
	u := st.SubjectUsage("cp:user:1")
	if u.Up != 15 || u.Down != 25 || u.Total != 40 {
		t.Fatalf("usage=%+v", u)
	}
	st2, err := store.New(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	u2 := st2.SubjectUsage("cp:user:1")
	if u2.Total != 40 {
		t.Fatalf("reload usage=%+v", u2)
	}
	if _, err := st2.PurgeOlderThan(now.AddDate(0, 0, 10)); err != nil {
		t.Fatal(err)
	}
	_ = filepath.Join(dir, "series")
}
