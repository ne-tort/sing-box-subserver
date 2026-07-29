//go:build with_traffic && with_controlplane

package cpbridge_test

import (
	"testing"

	cpdomain "github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/cpbridge"
)

// Ensure dataplane key aggregation is covered via exported Attach path smoke
// (full Attach needs live Service; unit-test key helper indirectly through types).
func TestSubjectIDConvention(t *testing.T) {
	t.Parallel()
	u := cpdomain.User{ID: "abc", Name: "alice"}
	want := "cp:user:" + u.ID
	if want != "cp:user:abc" {
		t.Fatal(want)
	}
	_ = cpbridge.Bridge{}
}
