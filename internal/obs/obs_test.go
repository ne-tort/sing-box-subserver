package obs

import (
	"strings"
	"testing"
)

func TestRingQueryAndCap(t *testing.T) {
	t.Parallel()
	r := NewRing(3)
	r.Append("info", "a", nil)
	r.Append("error", "b", nil)
	r.Append("info", "c", nil)
	r.Append("info", "d", nil) // drops a

	entries, next := r.Query(0, "", 10)
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
	if entries[0].Msg != "b" {
		t.Fatalf("oldest should be b, got %q", entries[0].Msg)
	}
	if next != entries[len(entries)-1].Seq {
		t.Fatalf("next mismatch")
	}

	errOnly, _ := r.Query(0, "error", 10)
	if len(errOnly) != 1 || errOnly[0].Msg != "b" {
		t.Fatalf("level filter: %+v", errOnly)
	}
}

func TestMetricsPrometheus(t *testing.T) {
	t.Parallel()
	m := &Metrics{}
	m.IncApply(true)
	m.IncApply(false)
	m.RollbackTotal.Add(1)
	text := m.PrometheusText()
	for _, want := range []string{
		`subserver_apply_total{result="ok"} 1`,
		`subserver_apply_total{result="fail"} 1`,
		`subserver_rollback_total 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}
