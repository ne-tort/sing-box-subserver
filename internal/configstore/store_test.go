package configstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteStagedPromote(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	raw := []byte(`{"inbounds":[]}`)
	meta, err := store.WriteStaged(raw, SourcePush)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ContentSHA256 != Hash(raw) {
		t.Fatalf("sha mismatch")
	}
	if meta.Revision != 0 {
		t.Fatalf("staged revision should be current (0), got %d", meta.Revision)
	}

	got, gotMeta, err := store.ReadStaged()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) || gotMeta.Source != SourcePush {
		t.Fatalf("staged read mismatch")
	}

	if store.HasLastGood() {
		t.Fatal("no last-good yet")
	}

	promoted, err := store.PromoteStaged()
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Revision != 1 {
		t.Fatalf("want revision 1, got %d", promoted.Revision)
	}

	lg, lgMeta, err := store.ReadLastGood()
	if err != nil {
		t.Fatal(err)
	}
	if string(lg) != string(raw) || lgMeta.Revision != 1 {
		t.Fatalf("last-good mismatch: %s meta=%+v", lg, lgMeta)
	}

	rev, err := store.CurrentRevision()
	if err != nil || rev != 1 {
		t.Fatalf("revision: %d err=%v", rev, err)
	}

	// Second promote bumps again
	raw2 := []byte(`{"inbounds":[{"type":"direct","tag":"d"}]}`)
	if _, err := store.WriteStaged(raw2, SourcePull); err != nil {
		t.Fatal(err)
	}
	promoted2, err := store.PromoteStaged()
	if err != nil {
		t.Fatal(err)
	}
	if promoted2.Revision != 2 || promoted2.Source != SourcePull {
		t.Fatalf("second promote: %+v", promoted2)
	}
}

func TestAtomicWriteLeavesNoTmp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteStaged([]byte(`{}`), SourceBoot); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == "" && len(e.Name()) > 0 && e.Name()[0] == '.' {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}
