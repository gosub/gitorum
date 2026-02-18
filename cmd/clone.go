package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"
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

	if _, err := os.Stat(filepath.Join(absDir, "GITORUM.toml")); err != nil {
		fmt.Fprintln(os.Stderr, "Warning: GITORUM.toml not found; this may not be a Gitorum forum.")
	} else {
		fmt.Fprintf(os.Stderr, "Done. Run: gitorum serve --repo %s\n", absDir)
	}
	return nil
}
