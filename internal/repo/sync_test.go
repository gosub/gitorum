package repo_test

import (
	"errors"
	"io/fs"
	"testing"

	gogit "github.com/go-git/go-git/v5"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/repo"
)

// twoClones returns alice's forum (pushed to a bare remote) and a clone of it
// opened as bob's repository.
func twoClones(t *testing.T) (alice, bob *repo.Repo, aliceID, bobID *crypto.Identity) {
	t.Helper()
	alice, aliceID = newForum(t, "alice")
	bare := newBareRemote(t)
	if err := alice.AddRemote("origin", bare); err != nil {
		t.Fatal(err)
	}
	if _, err := alice.Sync(); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	bobDir := t.TempDir()
	if _, err := gogit.PlainClone(bobDir, false, &gogit.CloneOptions{URL: bare}); err != nil {
		t.Fatal(err)
	}
	bob, err := repo.Open(bobDir)
	if err != nil {
		t.Fatal(err)
	}
	return alice, bob, aliceID, newIdentity(t, "bob")
}

func commitFile(t *testing.T, r *repo.Repo, id *crypto.Identity, path, content string) {
	t.Helper()
	if err := r.Commit(id, "post: "+path, map[string][]byte{path: []byte(content)}, nil); err != nil {
		t.Fatal(err)
	}
}

func hasFile(t *testing.T, r *repo.Repo, path string) bool {
	t.Helper()
	snap, err := r.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	_, err = fs.Stat(snap.FS, path)
	return err == nil
}

// TestSync_DivergedHistories is the offline-first case: both participants
// post while offline, then both sync. Neither sync may fail.
func TestSync_DivergedHistories(t *testing.T) {
	alice, bob, aliceID, bobID := twoClones(t)

	commitFile(t, alice, aliceID, "general/a/0000_root.md", "alice offline 1")
	commitFile(t, alice, aliceID, "general/a/1_aaaaaaaa.md", "alice offline 2")
	commitFile(t, bob, bobID, "general/b/0000_root.md", "bob offline")

	if _, err := bob.Sync(); err != nil {
		t.Fatalf("bob sync: %v", err)
	}
	if st := alice.Status(); st.Ahead != 2 {
		t.Errorf("alice ahead before sync: got %d, want 2", st.Ahead)
	}

	res, err := alice.Sync()
	if err != nil {
		t.Fatalf("alice sync after divergence: %v", err)
	}
	if res.Pulled != 1 || res.Rebased != 2 || !res.Pushed {
		t.Errorf("result: %+v, want pulled=1 rebased=2 pushed", res)
	}
	if st := alice.Status(); !st.Synced() {
		t.Errorf("alice status after sync: %+v", st)
	}

	if _, err := bob.Sync(); err != nil {
		t.Fatalf("bob second sync: %v", err)
	}
	for _, r := range []*repo.Repo{alice, bob} {
		for _, p := range []string{"general/a/0000_root.md", "general/a/1_aaaaaaaa.md", "general/b/0000_root.md"} {
			if !hasFile(t, r, p) {
				t.Errorf("%s missing %s", r.Path, p)
			}
		}
	}
	if alice.Head() != bob.Head() {
		t.Error("alice and bob ended on different commits")
	}
}

func TestSync_ConflictLeavesLocalUntouched(t *testing.T) {
	alice, bob, aliceID, bobID := twoClones(t)

	commitFile(t, bob, bobID, "GITORUM.toml", "name = \"Bob's\"\n")
	if _, err := bob.Sync(); err != nil {
		t.Fatal(err)
	}
	commitFile(t, alice, aliceID, "general/a/0000_root.md", "safe")
	commitFile(t, alice, aliceID, "GITORUM.toml", "name = \"Alice's\"\n")
	before := alice.Head()

	_, err := alice.Sync()
	if !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	if alice.Head() != before {
		t.Error("local branch moved after a failed sync")
	}
	if !hasFile(t, alice, "general/a/0000_root.md") {
		t.Error("local post lost after conflict")
	}
}

func TestSync_FastForwardAndStatus(t *testing.T) {
	alice, bob, _, bobID := twoClones(t)
	commitFile(t, bob, bobID, "general/b/0000_root.md", "hi")
	if _, err := bob.Sync(); err != nil {
		t.Fatal(err)
	}
	res, err := alice.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if res.Pulled != 1 || res.Rebased != 0 || res.Pushed {
		t.Errorf("result: %+v, want pulled=1 only", res)
	}
	if !hasFile(t, alice, "general/b/0000_root.md") {
		t.Error("fast-forwarded file missing")
	}
}

func TestSync_NoRemote(t *testing.T) {
	r, _ := newForum(t, "alice")
	if _, err := r.Sync(); err != nil {
		t.Errorf("Sync without remote: %v", err)
	}
	if st := r.Status(); !st.Synced() || st.RemoteURL != "" {
		t.Errorf("Status without remote: %+v", st)
	}
}
