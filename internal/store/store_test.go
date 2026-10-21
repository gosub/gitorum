package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/forum"
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
	if err := st.AddKey(admin, "alice", mallory.PublicKey, false); !errors.Is(err, store.ErrExists) {
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
	if _, err := st.Reply(admin, "general", "hello", "a reply", ""); err != nil {
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
		if _, err := st.Reply(admin, "general", "hello", "+1", ""); err != nil {
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

// ---- trust ----

func lastPostStatus(t *testing.T, st *store.Store, cat, slug string) forum.SigStatus {
	t.Helper()
	th, err := st.Thread(cat, slug)
	if err != nil {
		t.Fatal(err)
	}
	return th.Posts[len(th.Posts)-1].SigStatus
}

func TestTrust_ApprovedKeyIsCertified(t *testing.T) {
	st, admin := newStore(t)
	bob := newIdentity(t, "bob")
	if err := st.SubmitJoinRequest(bob); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveJoinRequest(admin, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := st.NewThread(bob, "general", "hi", "# Hi"); err != nil {
		t.Fatal(err)
	}
	if got := lastPostStatus(t, st, "general", "hi"); got != forum.SigValid {
		t.Errorf("approved user's post: %v, want SigValid", got)
	}
}

func TestTrust_PlantedKeyIsUntrusted(t *testing.T) {
	st, admin := newStore(t)
	if err := st.NewThread(admin, "general", "hi", "# Hi"); err != nil {
		t.Fatal(err)
	}
	// Anyone with push access writes their own key file and posts.
	mallory := newIdentity(t, "mallory")
	if err := st.Repo().Commit(mallory, "sneak", map[string][]byte{
		"keys/mallory.pub": []byte(mallory.PublicKey + "\n"),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Reply(mallory, "general", "hi", "trust me", ""); err != nil {
		t.Fatal(err)
	}
	if got := lastPostStatus(t, st, "general", "hi"); got != forum.SigUntrusted {
		t.Errorf("planted key post: %v, want SigUntrusted", got)
	}

	// Overwriting alice's key does not let mallory impersonate her either.
	if err := st.Repo().Commit(mallory, "sneak", map[string][]byte{
		"keys/alice.pub": []byte(mallory.PublicKey + "\n"),
	}, nil); err != nil {
		t.Fatal(err)
	}
	fake := &crypto.Identity{Username: "alice", PublicKey: mallory.PublicKey, PrivateKey: mallory.PrivateKey}
	if _, err := st.Reply(fake, "general", "hi", "I am alice", ""); err != nil {
		t.Fatal(err)
	}
	if got := lastPostStatus(t, st, "general", "hi"); got == forum.SigValid {
		t.Error("impersonation via swapped key file shows as valid")
	}

	problems, err := st.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) < 3 {
		t.Errorf("Verify found %d problems, want at least 3: %+v", len(problems), problems)
	}

	// After the admin certifies mallory (having checked out of band), her
	// posts are trusted.
	if err := st.Certify(admin, "mallory"); err != nil {
		t.Fatal(err)
	}
	th, _ := st.Thread("general", "hi")
	if th.Posts[1].SigStatus != forum.SigValid {
		t.Errorf("after certify: %v", th.Posts[1].SigStatus)
	}
}

func TestTrust_PinnedAdminSurvivesMetaChange(t *testing.T) {
	st, admin := newStore(t)
	mallory := newIdentity(t, "mallory")
	meta, _ := st.Meta()
	meta.AdminPubkey = mallory.PublicKey
	data, _ := meta.Encode()
	if err := st.Repo().Commit(mallory, "take over", map[string][]byte{repo.MetaFile: data}, nil); err != nil {
		t.Fatal(err)
	}
	if st.IsAdmin(mallory) {
		t.Error("mallory became admin by editing GITORUM.toml")
	}
	if !st.IsAdmin(admin) {
		t.Error("pinned admin lost admin rights")
	}
	if !st.AdminKeyMismatch() {
		t.Error("mismatch not reported")
	}
	if err := st.CreateCategory(mallory, "evil", "Evil", ""); !errors.Is(err, store.ErrForbidden) {
		t.Errorf("mallory CreateCategory: %v, want ErrForbidden", err)
	}
}

func TestTrust_TOFUPinsOnFirstUse(t *testing.T) {
	st, admin := newStore(t)
	// Simulate a clone made with plain git: no pin in .git/config.
	cfg := filepath.Join(st.Repo().Path, ".git", "config")
	data, _ := os.ReadFile(cfg)
	cleaned := strings.Split(string(data), "[gitorum]")[0]
	if err := os.WriteFile(cfg, []byte(cleaned), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := repo.Open(st.Repo().Path)
	if err != nil {
		t.Fatal(err)
	}
	if r.PinnedAdminKey() != "" {
		t.Fatal("pin not removed by test setup")
	}
	st2 := store.New(r)
	if !st2.IsAdmin(admin) {
		t.Error("admin not recognised after TOFU")
	}
	if r.PinnedAdminKey() != admin.PublicKey {
		t.Error("admin key not pinned on first use")
	}
}

func TestAddKey_Rotate(t *testing.T) {
	st, admin := newStore(t)
	bobOld, bobNew := newIdentity(t, "bob"), newIdentity(t, "bob")
	if err := st.AddKey(admin, "bob", bobOld.PublicKey, false); err != nil {
		t.Fatal(err)
	}
	if err := st.NewThread(bobOld, "general", "old", "# Old"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddKey(admin, "bob", bobNew.PublicKey, true); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if err := st.AddKey(admin, "bob", bobNew.PublicKey, true); !errors.Is(err, store.ErrExists) {
		t.Errorf("duplicate rotate: %v", err)
	}
	if err := st.AddKey(admin, "nobody", bobNew.PublicKey, true); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("rotate unknown user: %v", err)
	}
	if err := st.NewThread(bobNew, "general", "new", "# New"); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"old", "new"} {
		if got := lastPostStatus(t, st, "general", slug); got != forum.SigValid {
			t.Errorf("%s: %v, want SigValid", slug, got)
		}
	}
}

// ---- hide / purge ----

func TestHide_RootNotLeakedInList(t *testing.T) {
	st, admin := newStore(t)
	if err := st.NewThread(admin, "general", "secret", "# Secret title\n\nleaked?"); err != nil {
		t.Fatal(err)
	}
	if err := st.Tombstone(admin, "general", "secret", forum.RootFilename, false); err != nil {
		t.Fatal(err)
	}
	_, scans, err := st.Threads("general")
	if err != nil || len(scans) != 1 {
		t.Fatalf("Threads: %v %v", scans, err)
	}
	if !scans[0].Hidden || scans[0].Root != nil {
		t.Errorf("hidden root leaked in thread list: %+v", scans[0])
	}
}

func TestPurge_RemovesFileKeepsPlaceholder(t *testing.T) {
	st, admin := newStore(t)
	if err := st.NewThread(admin, "general", "t", "# T"); err != nil {
		t.Fatal(err)
	}
	name, err := st.Reply(admin, "general", "t", "spam spam spam", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Tombstone(admin, "general", "t", name, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(st.Repo().Path, "general", "t", name)); err == nil {
		t.Error("purged file still in working tree")
	}
	th, err := st.Thread("general", "t")
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Posts) != 2 || !th.Posts[1].Tombstoned {
		t.Errorf("want root + [deleted] placeholder, got %+v", th.Posts)
	}
	_, scans, _ := st.Threads("general")
	if scans[0].ReplyCount != 0 {
		t.Errorf("purged reply counted: %d", scans[0].ReplyCount)
	}
	if problems, _ := st.Verify(); len(problems) != 0 {
		t.Errorf("Verify after purge: %+v", problems)
	}
}

// ---- signed join requests ----

func TestJoinRequest_RequiresProofOfPossession(t *testing.T) {
	st, admin := newStore(t)
	victim, mallory := newIdentity(t, "victim"), newIdentity(t, "mallory")

	// Mallory files a request under someone else's name and key, unsigned,
	// and one claiming her own key but with a signature by another key.
	victimFile, _, _ := forum.SignJoinRequest(victim)
	forged := []byte(mallory.PublicKey + "\n" + strings.Split(string(victimFile), "\n")[1] + "\n")
	if err := st.Repo().Commit(mallory, "requests", map[string][]byte{
		"requests/victim.pub":  []byte(victim.PublicKey + "\n"),
		"requests/mallory.pub": forged,
	}, nil); err != nil {
		t.Fatal(err)
	}
	reqs, _ := st.JoinRequests()
	if len(reqs) != 2 {
		t.Fatalf("got %d requests", len(reqs))
	}
	for _, r := range reqs {
		if r.Problem == "" {
			t.Errorf("@%s: forged request accepted as valid", r.Username)
		}
		if err := st.ApproveJoinRequest(admin, r.Username); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("@%s approve: got %v, want ErrInvalid", r.Username, err)
		}
	}
}

func TestJoinToken(t *testing.T) {
	st, admin := newStore(t)
	bob := newIdentity(t, "bob")
	_, token, err := forum.SignJoinRequest(bob)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(token, ":bob:", ":rob:", 1)
	if _, err := st.AddKeyFromToken(admin, tampered); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("tampered token: got %v, want ErrInvalid", err)
	}
	user, err := st.AddKeyFromToken(admin, token)
	if err != nil || user != "bob" {
		t.Fatalf("AddKeyFromToken: %q %v", user, err)
	}
	if err := st.NewThread(bob, "general", "hi", "# hi"); err != nil {
		t.Fatal(err)
	}
	if got := lastPostStatus(t, st, "general", "hi"); got != forum.SigValid {
		t.Errorf("post after token approval: %v", got)
	}
}

func TestReply_ToSpecificPost(t *testing.T) {
	st, admin := newStore(t)
	if err := st.NewThread(admin, "general", "t", "# T"); err != nil {
		t.Fatal(err)
	}
	first, err := st.Reply(admin, "general", "t", "first", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Reply(admin, "general", "t", "answering first", first); err != nil {
		t.Fatal(err)
	}
	th, _ := st.Thread("general", "t")
	byName := map[string]*forum.Post{}
	for _, p := range th.Posts {
		byName[p.Filename] = p
	}
	if th.Posts[1].Parent != th.Root.Hash {
		t.Error("default reply does not point at the root")
	}
	if th.Posts[2].Parent != byName[first].Hash {
		t.Error("reply_to post does not point at the answered post")
	}
	for _, bad := range []string{"../../GITORUM.toml", "1700000000000_deadbeef.md"} {
		if _, err := st.Reply(admin, "general", "t", "x", bad); err == nil {
			t.Errorf("reply_to %q accepted", bad)
		}
	}
}
