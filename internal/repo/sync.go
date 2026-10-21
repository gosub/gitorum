package repo

import (
	"context"
	"errors"
	"fmt"
	"io"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/utils/merkletrie"
)

// ErrConflict is returned by Sync when a local commit cannot be replayed on
// top of the remote because both sides changed the same file differently.
// The local branch is left exactly as it was before the sync.
var ErrConflict = errors.New("sync conflict")

// SyncResult describes what a Sync did.
type SyncResult struct {
	Pulled  int // remote commits integrated
	Rebased int // local commits replayed on top of the remote
	Pushed  bool
}

// SyncStatus describes the local branch relative to the last-fetched remote.
type SyncStatus struct {
	RemoteURL string // "" when no origin is configured
	Ahead     int    // local commits not on the remote
	Behind    int    // fetched remote commits not yet integrated
}

// Synced reports whether there is nothing to push or integrate.
func (s SyncStatus) Synced() bool { return s.Ahead == 0 && s.Behind == 0 }

// Sync integrates origin into the current branch and pushes local commits.
//
// Forum commits almost always add new, uniquely named files, so instead of
// merging, local commits that are not on the remote are replayed on top of
// it (like git pull --rebase). If the push is rejected because someone else
// pushed in the meantime, the whole cycle is retried once.
func (r *Repo) Sync() (SyncResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var res SyncResult
	if r.remoteURL() == "" {
		return res, nil
	}
	for attempt := 0; ; attempt++ {
		if err := r.fetch(); err != nil {
			return res, err
		}
		pulled, rebased, err := r.integrate()
		res.Pulled += pulled
		res.Rebased += rebased
		if err != nil {
			return res, err
		}
		pushed, err := r.push()
		if errors.Is(err, gogit.ErrNonFastForwardUpdate) && attempt == 0 {
			continue
		}
		if err != nil {
			return res, fmt.Errorf("push: %w", err)
		}
		res.Pushed = pushed
		return res, nil
	}
}

// Status reports how far the local branch is from the last-fetched remote.
// It does not touch the network.
func (r *Repo) Status() SyncStatus {
	r.mu.Lock()
	defer r.mu.Unlock()

	st := SyncStatus{RemoteURL: r.remoteURL()}
	if st.RemoteURL == "" {
		return st
	}
	head, err := r.git.Head()
	if err != nil {
		return st
	}
	remote := plumbing.ZeroHash
	if ref, err := r.remoteRef(head.Name()); err == nil {
		remote = ref.Hash()
	}
	key := [2]plumbing.Hash{head.Hash(), remote}
	if r.statusKey == key {
		st.Ahead, st.Behind = r.statusCache[0], r.statusCache[1]
		return st
	}
	localSet, _ := r.ancestors(head.Hash())
	remoteSet, _ := r.ancestors(remote)
	for h := range localSet {
		if _, ok := remoteSet[h]; !ok {
			st.Ahead++
		}
	}
	for h := range remoteSet {
		if _, ok := localSet[h]; !ok {
			st.Behind++
		}
	}
	r.statusKey, r.statusCache = key, [2]int{st.Ahead, st.Behind}
	return st
}

// ---- internal ----

func (r *Repo) remoteRef(branch plumbing.ReferenceName) (*plumbing.Reference, error) {
	return r.git.Reference(plumbing.NewRemoteReferenceName("origin", branch.Short()), true)
}

func (r *Repo) ancestors(from plumbing.Hash) (map[plumbing.Hash]struct{}, error) {
	set := map[plumbing.Hash]struct{}{}
	if from.IsZero() {
		return set, nil
	}
	iter, err := r.git.Log(&gogit.LogOptions{From: from})
	if err != nil {
		return set, err
	}
	err = iter.ForEach(func(c *object.Commit) error {
		set[c.Hash] = struct{}{}
		return nil
	})
	return set, err
}

func (r *Repo) fetch() error {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	err := r.git.FetchContext(ctx, &gogit.FetchOptions{RemoteName: "origin"})
	switch {
	case err == nil, errors.Is(err, gogit.NoErrAlreadyUpToDate), errors.Is(err, transport.ErrEmptyRemoteRepository):
		return nil
	default:
		return fmt.Errorf("fetch: %w", err)
	}
}

// push returns pushed=false when the remote was already up to date.
func (r *Repo) push() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	err := r.git.PushContext(ctx, &gogit.PushOptions{RemoteName: "origin"})
	if errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return false, nil
	}
	return err == nil, err
}

// integrate brings the fetched remote branch into the local branch by
// fast-forwarding or by replaying local-only commits on top of it.
func (r *Repo) integrate() (pulled, rebased int, err error) {
	head, err := r.git.Head()
	if err != nil {
		return 0, 0, fmt.Errorf("head: %w", err)
	}
	remoteRef, err := r.remoteRef(head.Name())
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return 0, 0, nil // remote branch does not exist yet; push will create it
	}
	if err != nil {
		return 0, 0, fmt.Errorf("remote ref: %w", err)
	}
	if head.Hash() == remoteRef.Hash() {
		return 0, 0, nil
	}

	local, err := r.git.CommitObject(head.Hash())
	if err != nil {
		return 0, 0, err
	}
	remote, err := r.git.CommitObject(remoteRef.Hash())
	if err != nil {
		return 0, 0, err
	}

	if ok, err := remote.IsAncestor(local); err != nil {
		return 0, 0, err
	} else if ok {
		return 0, 0, nil // only local commits: nothing to integrate
	}

	bases, err := local.MergeBase(remote)
	if err != nil {
		return 0, 0, fmt.Errorf("merge base: %w", err)
	}
	if len(bases) == 0 {
		return 0, 0, fmt.Errorf("%w: local and remote histories are unrelated", ErrConflict)
	}
	base := bases[0].Hash

	pulled, err = r.countBetween(remote.Hash, base)
	if err != nil {
		return 0, 0, err
	}

	toReplay, err := r.firstParentChain(local, base)
	if err != nil {
		return 0, 0, err
	}
	if err := r.resetHard(remote.Hash); err != nil {
		return 0, 0, err
	}
	for _, c := range toReplay {
		if err := r.replay(c); err != nil {
			if rerr := r.resetHard(local.Hash); rerr != nil {
				return 0, 0, fmt.Errorf("%v (and restoring local branch failed: %v)", err, rerr)
			}
			return 0, 0, err
		}
		rebased++
	}
	return pulled, rebased, nil
}

// countBetween counts commits reachable from tip but not from base.
func (r *Repo) countBetween(tip, base plumbing.Hash) (int, error) {
	baseSet, err := r.ancestors(base)
	if err != nil {
		return 0, err
	}
	tipSet, err := r.ancestors(tip)
	if err != nil {
		return 0, err
	}
	n := 0
	for h := range tipSet {
		if _, ok := baseSet[h]; !ok {
			n++
		}
	}
	return n, nil
}

// firstParentChain returns the commits from base (exclusive) to tip
// (inclusive), oldest first. Merge commits are not supported.
func (r *Repo) firstParentChain(tip *object.Commit, base plumbing.Hash) ([]*object.Commit, error) {
	var chain []*object.Commit
	for c := tip; c.Hash != base; {
		if c.NumParents() != 1 {
			return nil, fmt.Errorf("%w: cannot replay merge or root commit %s", ErrConflict, c.Hash)
		}
		chain = append(chain, c)
		p, err := c.Parent(0)
		if err != nil {
			return nil, err
		}
		c = p
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

func (r *Repo) resetHard(h plumbing.Hash) error {
	head, err := r.git.Head()
	if err != nil {
		return fmt.Errorf("head: %w", err)
	}
	if err := r.git.Storer.SetReference(plumbing.NewHashReference(head.Name(), h)); err != nil {
		return fmt.Errorf("move branch to %s: %w", h, err)
	}
	wt, err := r.git.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	if err := wt.Reset(&gogit.ResetOptions{Commit: h, Mode: gogit.HardReset}); err != nil {
		return fmt.Errorf("reset to %s: %w", h, err)
	}
	return nil
}

// replay applies the changes c made to its parent on top of the current
// HEAD and commits them with c's author and message.
func (r *Repo) replay(c *object.Commit) error {
	parent, err := c.Parent(0)
	if err != nil {
		return err
	}
	fromTree, err := parent.Tree()
	if err != nil {
		return err
	}
	toTree, err := c.Tree()
	if err != nil {
		return err
	}
	changes, err := object.DiffTree(fromTree, toTree)
	if err != nil {
		return fmt.Errorf("diff %s: %w", c.Hash, err)
	}
	head, err := r.git.Head()
	if err != nil {
		return err
	}
	headCommit, err := r.git.CommitObject(head.Hash())
	if err != nil {
		return err
	}
	cur, err := headCommit.Tree()
	if err != nil {
		return err
	}

	write := map[string][]byte{}
	var remove []string
	for _, ch := range changes {
		action, err := ch.Action()
		if err != nil {
			return err
		}
		from, to, err := ch.Files()
		if err != nil {
			return err
		}
		// File.Name is only the base name; ChangeEntry.Name is the full path.
		toPath, fromPath := ch.To.Name, ch.From.Name
		switch action {
		case merkletrie.Insert, merkletrie.Modify:
			want, err := contents(to)
			if err != nil {
				return err
			}
			have, present, err := treeContents(cur, toPath)
			if err != nil {
				return err
			}
			switch {
			case present && have == want:
				// already applied upstream
			case action == merkletrie.Insert && present:
				return fmt.Errorf("%w: %s was added on both sides with different content", ErrConflict, toPath)
			case action == merkletrie.Modify:
				old, _ := contents(from)
				if !present || have != old {
					return fmt.Errorf("%w: %s was changed on both sides", ErrConflict, toPath)
				}
				write[toPath] = []byte(want)
			default:
				write[toPath] = []byte(want)
			}
		case merkletrie.Delete:
			old, err := contents(from)
			if err != nil {
				return err
			}
			have, present, err := treeContents(cur, fromPath)
			if err != nil {
				return err
			}
			switch {
			case !present:
				// already deleted upstream
			case have != old:
				return fmt.Errorf("%w: %s was deleted locally but changed upstream", ErrConflict, fromPath)
			default:
				remove = append(remove, fromPath)
			}
		}
	}
	if len(write) == 0 && len(remove) == 0 {
		return nil
	}
	author := c.Author
	committer := c.Committer
	committer.When = nowFunc()
	return r.commitAs(&author, &committer, c.Message, write, remove)
}

func contents(f *object.File) (string, error) {
	if f == nil {
		return "", nil
	}
	return f.Contents()
}

func treeContents(t *object.Tree, path string) (string, bool, error) {
	f, err := t.File(path)
	if errors.Is(err, object.ErrFileNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	s, err := f.Contents()
	if err != nil && !errors.Is(err, io.EOF) {
		return "", false, err
	}
	return s, true, nil
}
