package version

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildTagsSyncWithFile(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "build", "tags.server")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(strings.ReplaceAll(string(raw), "\r", ""))
	if want != DefaultBuildTags {
		t.Fatalf("DefaultBuildTags out of sync with build/tags.server\n want %q\n got  %q", want, DefaultBuildTags)
	}
}
