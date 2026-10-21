package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"

	"github.com/gosub/gitorum/internal/forum"
	"github.com/gosub/gitorum/internal/repo"
	"github.com/gosub/gitorum/internal/store"
)

var cloneCmd = &cobra.Command{
	Use:   "clone <url> [dir]",
	Short: "Clone a remote Gitorum forum repository",
	Long: `Clone a remote forum repository to a local directory.

The directory defaults to the last component of the URL with .git stripped.
After cloning, run 'gitorum serve --repo <dir>' to start the web server.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runClone,
}

func init() {
	rootCmd.AddCommand(cloneCmd)
}

func runClone(cmd *cobra.Command, args []string) error {
	url := args[0]

	dir := ""
	if len(args) > 1 {
		dir = args[1]
	} else {
		base := filepath.Base(url)
		base = strings.TrimSuffix(base, ".git")
		if base == "" || base == "." {
			return fmt.Errorf("cannot derive directory name from URL; pass a directory as the second argument")
		}
		dir = base
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Cloning %s into %s…\n", url, absDir)
	_, err = gogit.PlainClone(absDir, false, &gogit.CloneOptions{
		URL:      url,
		Progress: os.Stderr,
	})
	if err != nil {
		return fmt.Errorf("clone: %w", err)
	}

	r, err := repo.Open(absDir)
	if err != nil {
		return err
	}
	meta, err := store.New(r).Meta()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Warning: GITORUM.toml not found; this may not be a Gitorum forum.")
		return nil
	}
	// Pin the admin key now (trust on first use). Later changes to
	// GITORUM.toml in the shared repo cannot replace it.
	if err := r.PinAdminKey(meta.AdminPubkey); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Pinned admin key %s. Confirm it with the forum admin out of band.\n",
		forum.Fingerprint(meta.AdminPubkey))
	fmt.Fprintf(os.Stderr, "Done. Run: gitorum serve --repo %s\n", absDir)
	return nil
}
