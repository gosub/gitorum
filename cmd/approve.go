package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/repo"
	"github.com/gosub/gitorum/internal/store"
)

var approveCmd = &cobra.Command{
	Use:   "approve <username | join-token>",
	Short: "Approve a join request (admin only)",
	Long: `Register and certify a user's key.

The argument is either the username of a pending request in requests/, or
a token produced by 'gitorum request --print'. Either way the request's
signature is checked before the key is added.`,
	Args: cobra.ExactArgs(1),
	RunE: runApprove,
}

var (
	approveRepoPath string
	approveIdentity string
	approveNoPush   bool
)

func init() {
	approveCmd.Flags().StringVar(&approveRepoPath, "repo", ".", "path to the forum git repository")
	approveCmd.Flags().StringVar(&approveIdentity, "identity", "", "path to identity file (default: "+defaultIdentityHint()+")")
	approveCmd.Flags().BoolVar(&approveNoPush, "no-push", false, "commit locally without syncing")
	rootCmd.AddCommand(approveCmd)
}

func runApprove(cmd *cobra.Command, args []string) error {
	identPath := approveIdentity
	if identPath == "" {
		identPath = crypto.DefaultIdentityPath()
	}
	id, err := crypto.LoadIdentity(identPath)
	if err != nil {
		return err
	}
	r, err := repo.Open(approveRepoPath)
	if err != nil {
		return err
	}
	st := store.New(r)

	username := args[0]
	if strings.HasPrefix(username, "gitorum-join:") {
		if username, err = st.AddKeyFromToken(id, args[0]); err != nil {
			return err
		}
	} else if err := st.ApproveJoinRequest(id, username); err != nil {
		return err
	}
	fmt.Printf("Approved @%s\n", username)
	if !approveNoPush {
		if _, err := r.Sync(); err != nil {
			return fmt.Errorf("committed locally, but sync failed: %w", err)
		}
	}
	return nil
}
