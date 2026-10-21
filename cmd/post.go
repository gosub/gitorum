package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/repo"
	"github.com/gosub/gitorum/internal/store"
)

var postCmd = &cobra.Command{
	Use:   "post <category>",
	Short: "Start a new thread",
	Long: `Create a signed thread in <category>. The body is read from --body,
from --file, or from standard input. The commit is synced unless --no-push.

  echo "Hello everyone" | gitorum post general --title "Introductions"`,
	Args: cobra.ExactArgs(1),
	RunE: runPost,
}

var replyCmd = &cobra.Command{
	Use:   "reply <category>/<thread>",
	Short: "Reply to a thread",
	Long: `Add a signed reply to a thread. The body is read from --body, from --file,
or from standard input. Use --to <post filename> to answer a specific post.`,
	Args: cobra.ExactArgs(1),
	RunE: runReply,
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Pull, replay local commits, and push",
	Long: `Fetch origin, integrate it (fast-forward, or replay local commits on top),
and push. When run by the admin on a forum with auto-approve enabled,
signed join requests are approved as part of the sync.`,
	Args: cobra.NoArgs,
	RunE: runSync,
}

// writeFlags are the flags shared by commands that create posts.
type writeFlags struct {
	repo, identity, body, file string
	noPush                     bool
}

func (f *writeFlags) register(c *cobra.Command) {
	c.Flags().StringVar(&f.repo, "repo", ".", "path to the forum git repository")
	c.Flags().StringVar(&f.identity, "identity", "", "path to identity file (default: "+defaultIdentityHint()+")")
	c.Flags().StringVar(&f.body, "body", "", "post body (Markdown)")
	c.Flags().StringVar(&f.file, "file", "", "read the post body from this file")
	c.Flags().BoolVar(&f.noPush, "no-push", false, "commit locally without syncing")
}

func (f *writeFlags) open() (*store.Store, *crypto.Identity, error) {
	id, err := loadIdentity(f.identity)
	if err != nil {
		return nil, nil, err
	}
	r, err := repo.Open(f.repo)
	if err != nil {
		return nil, nil, err
	}
	return store.New(r), id, nil
}

func (f *writeFlags) readBody() (string, error) {
	switch {
	case f.body != "":
		return f.body, nil
	case f.file != "":
		b, err := os.ReadFile(f.file)
		return string(b), err
	default:
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
}

func (f *writeFlags) share(st *store.Store) error {
	if f.noPush {
		return nil
	}
	if _, err := st.Repo().Sync(); err != nil {
		return fmt.Errorf("committed locally, but sync failed (run 'gitorum sync' to retry): %w", err)
	}
	return nil
}

var (
	postFlags  writeFlags
	postTitle  string
	postSlug   string
	replyFlags writeFlags
	replyTo    string
	syncRepo   string
	syncIdent  string
)

func init() {
	postFlags.register(postCmd)
	postCmd.Flags().StringVar(&postTitle, "title", "", "thread title (required)")
	postCmd.Flags().StringVar(&postSlug, "slug", "", "URL slug (default: derived from the title)")
	_ = postCmd.MarkFlagRequired("title")

	replyFlags.register(replyCmd)
	replyCmd.Flags().StringVar(&replyTo, "to", "", "filename of the post to answer (default: the thread's first post)")

	syncCmd.Flags().StringVar(&syncRepo, "repo", ".", "path to the forum git repository")
	syncCmd.Flags().StringVar(&syncIdent, "identity", "", "path to identity file (default: "+defaultIdentityHint()+")")

	categoryCmd.Flags().StringVar(&categoryRepo, "repo", ".", "path to the forum git repository")
	categoryCmd.Flags().StringVar(&categoryIdent, "identity", "", "path to identity file (default: "+defaultIdentityHint()+")")
	categoryCmd.Flags().StringVar(&categoryName, "name", "", "display name (required)")
	categoryCmd.Flags().StringVar(&categoryDesc, "description", "", "description")
	categoryCmd.Flags().BoolVar(&categoryNoPush, "no-push", false, "commit locally without syncing")
	_ = categoryCmd.MarkFlagRequired("name")

	rootCmd.AddCommand(postCmd, replyCmd, syncCmd, categoryCmd)
}

var categoryCmd = &cobra.Command{
	Use:   "category <slug>",
	Short: "Create a category (admin only)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		f := writeFlags{repo: categoryRepo, identity: categoryIdent, noPush: categoryNoPush}
		st, id, err := f.open()
		if err != nil {
			return err
		}
		if err := st.CreateCategory(id, args[0], categoryName, categoryDesc); err != nil {
			return err
		}
		fmt.Printf("Created category %s\n", args[0])
		return f.share(st)
	},
}

var (
	categoryRepo, categoryIdent, categoryName, categoryDesc string
	categoryNoPush                                          bool
)

func runPost(cmd *cobra.Command, args []string) error {
	st, id, err := postFlags.open()
	if err != nil {
		return err
	}
	body, err := postFlags.readBody()
	if err != nil {
		return err
	}
	slug, err := st.NewThread(id, args[0], postTitle, postSlug, strings.TrimRight(body, "\n"))
	if err != nil {
		return err
	}
	fmt.Printf("Created %s/%s\n", args[0], slug)
	return postFlags.share(st)
}

func runReply(cmd *cobra.Command, args []string) error {
	cat, thread, ok := strings.Cut(args[0], "/")
	if !ok {
		return fmt.Errorf("expected <category>/<thread>, got %q", args[0])
	}
	st, id, err := replyFlags.open()
	if err != nil {
		return err
	}
	body, err := replyFlags.readBody()
	if err != nil {
		return err
	}
	name, err := st.Reply(id, cat, thread, strings.TrimRight(body, "\n"), replyTo)
	if err != nil {
		return err
	}
	fmt.Printf("Replied: %s/%s/%s\n", cat, thread, name)
	return replyFlags.share(st)
}

func runSync(cmd *cobra.Command, args []string) error {
	r, err := repo.Open(syncRepo)
	if err != nil {
		return err
	}
	// The identity is optional: it only matters for admin auto-approval.
	id, _ := loadIdentity(syncIdent)
	res, err := store.New(r).Sync(id)
	if err != nil {
		return err
	}
	fmt.Printf("Pulled %d, replayed %d local, pushed: %v\n", res.Pulled, res.Rebased, res.Pushed)
	return nil
}
