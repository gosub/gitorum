package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/repo"
	"github.com/gosub/gitorum/internal/store"
)

func newIdentity(t *testing.T, username string) *crypto.Identity {
	t.Helper()
	id, err := crypto.Generate(username)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// newStore creates a forum administered by alice with a "general" category.
func newStore(t *testing.T) (*store.Store, *crypto.Identity) {
	t.Helper()
	admin := newIdentity(t, "alice")
	r, err := repo.Init(t.TempDir(), repo.ForumMeta{Name: "F", AdminPubkey: admin.PublicKey}, admin)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(r)
	if err := st.CreateCategory(admin, "general", "General", ""); err != nil {
		t.Fatal(err)
	}
	return st, admin
}

func TestJoinRequests_Lifecycle(t *testing.T) {
	st, admin := newStore(t)
	bob, carol := newIdentity(t, "bob"), newIdentity(t, "carol")

	if reqs, _ := st.JoinRequests(); len(reqs) != 0 {
		t.Fatalf("expected no requests, got %v", reqs)
	}
	for _, id := range []*crypto.Identity{bob, carol} {
		if err := st.SubmitJoinRequest(id); err != nil {
			t.Fatal(err)
		}
	}
	reqs, err := st.JoinRequests()
	if err != nil || len(reqs) != 2 {
		t.Fatalf("JoinRequests: %v %v", reqs, err)
	}

	if err := st.ApproveJoinRequest(bob, "carol"); !errors.Is(err, store.ErrForbidden) {
		t.Errorf("non-admin approve: got %v, want ErrForbidden", err)
	}
	if err := st.ApproveJoinRequest(admin, "bob"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !st.HasKey("bob") {
		t.Error("bob has no key after approval")
	}
	if err := st.RejectJoinRequest(admin, "carol"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if st.HasKey("carol") {
		t.Error("carol has a key after rejection")
	}
	if reqs, _ := st.JoinRequests(); len(reqs) != 0 {
		t.Errorf("expected no pending requests, got %v", reqs)
	}
	if err := st.ApproveJoinRequest(admin, "dave"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("approve missing request: got %v, want ErrNotFound", err)
	}
	if err := st.SubmitJoinRequest(bob); !errors.Is(err, store.ErrExists) {
		t.Errorf("request for registered user: got %v, want ErrExists", err)
	}
}

func TestAddKey_NoOverwrite(t *testing.T) {
	st, admin := newStore(t)
	mallory := newIdentity(t, "alice")
	if err := st.AddKey(admin, "alice", mallory.PublicKey); !errors.Is(err, store.ErrExists) {
		t.Errorf("got %v, want ErrExists", err)
	}
}

func TestThreads_CacheFollowsHead(t *testing.T) {
	st, admin := newStore(t)
	if err := st.NewThread(admin, "general", "hello", "# Hello"); err != nil {
		t.Fatal(err)
	}
	th, err := st.Thread("general", "hello")
	if err != nil || len(th.Posts) != 1 {
		t.Fatalf("Thread: %v %v", th, err)
	}
	if _, err := st.Reply(admin, "general", "hello", "a reply"); err != nil {
		t.Fatal(err)
	}
	th, _ = st.Thread("general", "hello")
	if len(th.Posts) != 2 {
		t.Errorf("after reply: got %d posts, want 2 (stale cache?)", len(th.Posts))
	}
}

func TestReply_SameBodyTwice(t *testing.T) {
	st, admin := newStore(t)
	if err := st.NewThread(admin, "general", "hello", "# Hello"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := st.Reply(admin, "general", "hello", "+1"); err != nil {
			t.Fatalf("reply %d: %v", i, err)
		}
	}
	th, _ := st.Thread("general", "hello")
	if len(th.Posts) != 4 {
		t.Errorf("got %d posts, want 4", len(th.Posts))
	}
}

func TestThread_IgnoresUntrackedFiles(t *testing.T) {
	st, admin := newStore(t)
	if err := st.NewThread(admin, "general", "hello", "# Hello"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(st.Repo().Path, "general", "hello")
	_ = os.WriteFile(filepath.Join(dir, "1700000000000_deadbeef.md"), []byte("not committed"), 0o644)
	th, _ := st.Thread("general", "hello")
	if len(th.Posts) != 1 {
		t.Errorf("untracked post visible: got %d posts", len(th.Posts))
	}
}

func TestNotFound(t *testing.T) {
	st, _ := newStore(t)
	for _, tc := range [][2]string{{"general", "nope"}, {"..", "general"}, {"nope", "x"}} {
		if _, err := st.Thread(tc[0], tc[1]); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("Thread(%q,%q): got %v, want ErrNotFound", tc[0], tc[1], err)
		}
	}
	if _, _, err := st.Threads("nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Threads: got %v", err)
	}
}
