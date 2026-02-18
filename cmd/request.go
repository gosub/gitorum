package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/repo"
)

var requestCmd = &cobra.Command{
	Use:   "request",
	Short: "Submit a join request to a forum",
	Long: `Submit a join request by adding your public key to the requests/ directory.

The forum admin will see the request on their next sync and can approve or
reject it from the admin panel. Once approved, your posts will display a
signature-verified badge.

Your identity must already exist (run 'gitorum keygen' first).
You must have already cloned the forum repo (run 'gitorum clone' first).`,
	RunE: runRequest,
}

var (
	requestRepoPath string
	requestIdentity string
)

func init() {
	requestCmd.Flags().StringVar(&requestRepoPath, "repo", ".", "path to the forum git repository")
	requestCmd.Flags().StringVar(&requestIdentity, "identity", "", "path to identity file (default: "+defaultIdentityHint()+")")
	rootCmd.AddCommand(requestCmd)
}

func runRequest(cmd *cobra.Command, args []string) error {
	identPath := requestIdentity
	if identPath == "" {
		identPath = crypto.DefaultIdentityPath()
	}
	id, err := crypto.LoadIdentity(identPath)
	if err != nil {
		return fmt.Errorf("load identity: %w", err)
	}

	absRepo, err := filepath.Abs(requestRepoPath)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}
	r, err := repo.Open(absRepo)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	// Already approved?
	if _, err := os.Stat(filepath.Join(r.Path, "keys", id.Username+".pub")); err == nil {
		fmt.Printf("@%s is already approved in this forum.\n", id.Username)
		return nil
	}

	// Request already pending?
	if _, err := os.Stat(filepath.Join(r.Path, "requests", id.Username+".pub")); err == nil {
		fmt.Printf("A join request for @%s is already pending.\n", id.Username)
		return nil
	}

	if err := r.SubmitJoinRequest(id); err != nil {
		return fmt.Errorf("submit request: %w", err)
	}
	fmt.Printf("Join request submitted for @%s\n", id.Username)

	if err := r.Push(); err != nil {
		fmt.Fprintf(os.Stderr, "Note: push failed (%v); you can run 'git push' manually.\n", err)
	} else {
		fmt.Println("Pushed to remote. The forum admin will see the request on their next sync.")
	}
	return nil
}
