package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/forum"
	"github.com/gosub/gitorum/internal/repo"
	"github.com/gosub/gitorum/internal/store"
)

var requestCmd = &cobra.Command{
	Use:   "request",
	Short: "Submit a join request to a forum",
	Long: `Ask the forum admin to register your key.

By default the request is committed to requests/<username>.pub in a cloned
forum repository and pushed. That needs push access to the forum remote.

With --print, nothing is written: a one-line signed token is printed
instead. Send it to the admin by any channel (mail, chat); they paste it
into "Add user key" or run 'gitorum approve <token>'.

Both forms are signed with your key, proving you hold it.
Your identity must already exist (run 'gitorum keygen' first).`,
	RunE: runRequest,
}

var (
	requestRepoPath string
	requestIdentity string
	requestPrint    bool
)

func init() {
	requestCmd.Flags().StringVar(&requestRepoPath, "repo", ".", "path to the forum git repository")
	requestCmd.Flags().StringVar(&requestIdentity, "identity", "", "path to identity file (default: "+defaultIdentityHint()+")")
	requestCmd.Flags().BoolVar(&requestPrint, "print", false, "print a signed join token instead of committing a request")
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

	if requestPrint {
		_, token, err := forum.SignJoinRequest(id)
		if err != nil {
			return err
		}
		fmt.Println(token)
		return nil
	}

	absRepo, err := filepath.Abs(requestRepoPath)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}
	r, err := repo.Open(absRepo)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	st := store.New(r)
	if st.HasKey(id.Username) {
		fmt.Printf("@%s is already approved in this forum.\n", id.Username)
		return nil
	}
	reqs, err := st.JoinRequests()
	if err != nil {
		return err
	}
	for _, req := range reqs {
		if req.Username == id.Username {
			fmt.Printf("A join request for @%s is already pending.\n", id.Username)
			return nil
		}
	}

	if err := st.SubmitJoinRequest(id); err != nil {
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
