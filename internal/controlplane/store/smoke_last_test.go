//go:build with_controlplane

package store

import (
	"errors"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/smoke"
)

func TestSmokeLastPersistOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadSmokeLast(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound got %v", err)
	}
	lat := 42
	first := &smoke.Report{
		DurationMs: 100,
		FinishedAt: "2026-08-03T00:00:00Z",
		Results: []smoke.Result{{
			Set: "a", Preset: "vless-tcp", OK: true, LatencyMs: &lat,
		}},
	}
	if err := st.SaveSmokeLast(first); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadSmokeLast()
	if err != nil {
		t.Fatal(err)
	}
	if got.FinishedAt != first.FinishedAt || len(got.Results) != 1 || !got.Results[0].OK {
		t.Fatalf("got %+v", got)
	}
	second := &smoke.Report{
		DurationMs: 200,
		FinishedAt: "2026-08-03T01:00:00Z",
		Results: []smoke.Result{{
			Set: "b", Preset: "hy2", OK: false, Error: "timeout",
		}},
	}
	if err := st.SaveSmokeLast(second); err != nil {
		t.Fatal(err)
	}
	got, err = st.LoadSmokeLast()
	if err != nil {
		t.Fatal(err)
	}
	if got.FinishedAt != second.FinishedAt || got.Results[0].Set != "b" {
		t.Fatalf("overwrite failed: %+v", got)
	}
}
