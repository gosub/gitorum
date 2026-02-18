package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/repo"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View or update forum configuration",
	Long: `View or update the forum's metadata and git remote configuration.

Without flags, prints the current configuration.
Use --name to rename the forum and --remote to set the origin remote URL.`,
	RunE: runConfig,
}

var (
	configRepoPath  string
	configRemoteURL string
	configForumName string
	configIdentity  string
)

func init() {
	configCmd.Flags().StringVar(&configRepoPath, "repo", ".", "path to the forum git repository")
	configCmd.Flags().StringVar(&configRemoteURL, "remote", "", "set the origin remote URL")
	configCmd.Flags().StringVar(&configForumName, "name", "", "set the forum display name")
	configCmd.Flags().StringVar(&configIdentity, "identity", "", "path to identity file (default: "+defaultIdentityHint()+")")
	rootCmd.AddCommand(configCmd)
}

func runConfig(cmd *cobra.Command, args []string) error {
	r, err := repo.Open(configRepoPath)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	meta, err := r.ReadMeta()
	if err != nil {
		return fmt.Errorf("read metadata: %w", err)
	}

	// No flags → print current config.
	if configRemoteURL == "" && configForumName == "" {
		_, remoteURL := r.IsSynced()
		fmt.Printf("Forum name : %s\n", meta.Name)
		fmt.Printf("Admin key  : %s\n", meta.AdminPubkey)
		fmt.Printf("Remote URL : %s\n", remoteURL)
		return nil
	}

	identPath := configIdentity
	if identPath == "" {
		identPath = crypto.DefaultIdentityPath()
	}
	id, err := crypto.LoadIdentity(identPath)
	if err != nil {
		return fmt.Errorf("load identity: %w", err)
	}

	if configForumName != "" {
		meta.Name = configForumName
		if err := r.UpdateMeta(id, *meta); err != nil {
			return fmt.Errorf("update metadata: %w", err)
		}
		fmt.Printf("Forum name updated to %q\n", meta.Name)
	}

	if configRemoteURL != "" {
		if err := r.AddRemote("origin", configRemoteURL); err != nil {
			return fmt.Errorf("set remote: %w", err)
		}
		fmt.Printf("Remote URL set to %s\n", configRemoteURL)
	}

	return nil
}
