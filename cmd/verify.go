package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/repo"
	"github.com/gosub/gitorum/internal/store"
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Audit signatures, key certificates and tombstones",
	Long: `Check the whole forum at HEAD against the locally pinned admin key:

  - every keys/<user>.pub must be certified by the admin (keys/<user>.sig)
  - every post must carry a valid signature from a certified key
  - every tombstone must be signed by the admin and reference its post
  - GITORUM.toml must name the pinned admin key

Exits with status 1 if any problem is found.`,
	Args: cobra.NoArgs,
	RunE: runVerify,
}

var certifyCmd = &cobra.Command{
	Use:   "certify <username>...",
	Short: "Certify existing user keys with the admin key (admin only)",
	Long: `Sign keys/<username>.pub with the admin key so posts made with it are
shown as trusted. Use this to migrate forums created before key
certificates existed. Check each key with its owner out of band first:
certifying a key someone else planted lets them post under that name.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runCertify,
}

var (
	verifyRepoPath  string
	certifyRepoPath string
	certifyIdentity string
	certifyNoPush   bool
)

func init() {
	verifyCmd.Flags().StringVar(&verifyRepoPath, "repo", ".", "path to the forum git repository")
	certifyCmd.Flags().StringVar(&certifyRepoPath, "repo", ".", "path to the forum git repository")
	certifyCmd.Flags().StringVar(&certifyIdentity, "identity", "", "path to identity file (default: "+defaultIdentityHint()+")")
	certifyCmd.Flags().BoolVar(&certifyNoPush, "no-push", false, "commit locally without syncing")
	rootCmd.AddCommand(verifyCmd, certifyCmd)
}

func runVerify(cmd *cobra.Command, args []string) error {
	r, err := repo.Open(verifyRepoPath)
	if err != nil {
		return err
	}
	problems, err := store.New(r).Verify()
	if err != nil {
		return err
	}
	for _, p := range problems {
		fmt.Printf("%s: %s\n", p.Path, p.Message)
	}
	if len(problems) > 0 {
		cmd.SilenceUsage = true
		return fmt.Errorf("%d problem(s) found", len(problems))
	}
	fmt.Println("OK: all keys, posts and tombstones verify.")
	return nil
}

func runCertify(cmd *cobra.Command, args []string) error {
	identPath := certifyIdentity
	if identPath == "" {
		identPath = crypto.DefaultIdentityPath()
	}
	id, err := crypto.LoadIdentity(identPath)
	if err != nil {
		return err
	}
	r, err := repo.Open(certifyRepoPath)
	if err != nil {
		return err
	}
	if err := store.New(r).Certify(id, args...); err != nil {
		return err
	}
	fmt.Printf("Certified: %v\n", args)
	if !certifyNoPush {
		if _, err := r.Sync(); err != nil {
			return fmt.Errorf("committed locally, but sync failed: %w", err)
		}
	}
	return nil
}
