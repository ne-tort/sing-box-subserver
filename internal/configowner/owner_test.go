package configowner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaimMatrix(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var leftSub, leftCP int
	r.SetHooks(Hooks{
		OnLeaveSubscribe:    func() { leftSub++ },
		OnLeaveControlplane: func() { leftCP++ },
	})

	steps := []struct {
		to            Mode
		wantSub, wantCP int
	}{
		{ModeSubscribed, 0, 0},
		{ModeDirect, 1, 0},
		{ModeControlplane, 1, 0},
		{ModeIdle, 1, 1},
		{ModeSubscribed, 1, 1},
		{ModeSubscribed, 1, 1}, // noop
		{ModeControlplane, 2, 1},
		{ModeDirect, 2, 2},
	}
	for i, s := range steps {
		if err := r.Claim(s.to); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if r.Owner() != s.to {
			t.Fatalf("step %d: owner=%s want %s", i, r.Owner(), s.to)
		}
		if leftSub != s.wantSub || leftCP != s.wantCP {
			t.Fatalf("step %d: hooks sub=%d cp=%d want %d/%d", i, leftSub, leftCP, s.wantSub, s.wantCP)
		}
	}

	// Persist across reopen.
	r2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Owner() != ModeDirect {
		t.Fatalf("reopen: got %s", r2.Owner())
	}
	if _, err := filepath.Glob(filepath.Join(dir, stateFile)); err != nil {
		t.Fatal(err)
	}
}

func TestClaimPersistRollback(t *testing.T) {
	t.Parallel()
	// Open on a path that cannot be written after we make the file a directory clash.
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Claim(ModeDirect); err != nil {
		t.Fatal(err)
	}
	// Replace config-owner.json with a directory so persist fails.
	path := filepath.Join(dir, stateFile)
	_ = os.Remove(path)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := r.Claim(ModeSubscribed); err == nil {
		t.Fatal("expected persist error")
	}
	if r.Owner() != ModeDirect {
		t.Fatalf("rolled back want direct got %s", r.Owner())
	}
}
