package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ggeurts/gitorum/internal/crypto"
	"github.com/ggeurts/gitorum/internal/repo"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Gitorum forum repository",
	Long: `Initialize a new forum repository in the target directory.

Creates GITORUM.toml, the keys/ directory (with the admin's public key),
and makes the first git commit. The local identity is used as the forum admin.
If no identity file exists one is generated automatically.`,
	RunE: runInit,
}

var (
	initDir         string
	initName        string
	initDescription string
	initUsername    string
	initIdentityPath string
	initRemote      string
)

func init() {
	initCmd.Flags().StringVar(&initDir, "dir", ".", "directory to initialize the forum in")
	initCmd.Flags().StringVar(&initName, "name", "", "forum name (required)")
	initCmd.Flags().StringVar(&initDescription, "description", "", "forum description")
	initCmd.Flags().StringVar(&initUsername, "username", "", "admin username (default: taken from identity file)")
	initCmd.Flags().StringVar(&initIdentityPath, "identity", "", "path to identity file (default: "+crypto.DefaultIdentityPath()+")")
	initCmd.Flags().StringVar(&initRemote, "remote", "", "remote URL to configure as 'origin' (optional)")
	_ = initCmd.MarkFlagRequired("name")

	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	// Resolve identity path.
	identPath := initIdentityPath
	if identPath == "" {
		identPath = crypto.DefaultIdentityPath()
	}

	// Determine username: flag > identity file > error.
	username := initUsername
	if username == "" {
		// Try to peek at existing identity for its username.
		if existing, err := crypto.LoadIdentity(identPath); err == nil {
			username = existing.Username
		}
	}
	if username == "" {
		return fmt.Errorf("--username is required when no identity file exists yet")
	}

	// Load or create identity.
	id, created, err := crypto.LoadOrCreate(identPath, username)
	if err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	if created {
		fmt.Printf("Generated new identity for %q\n", id.Username)
		fmt.Printf("Public key : %s\n", id.PublicKey)
		fmt.Printf("Saved to   : %s\n", identPath)
	}

	// Resolve absolute path for the forum directory.
	dir := initDir
	if !filepath.IsAbs(dir) {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = filepath.Join(cwd, dir)
	}

	meta := repo.ForumMeta{
		Name:        initName,
		Description: initDescription,
		AdminPubkey: id.PublicKey,
	}

	r, err := repo.Init(dir, meta, id)
	if err != nil {
		return fmt.Errorf("init repo: %w", err)
	}

	if initRemote != "" {
		if err := r.AddRemote("origin", initRemote); err != nil {
			return fmt.Errorf("add remote: %w", err)
		}
		fmt.Printf("Remote     : origin → %s\n", initRemote)
	}

	fmt.Printf("Forum      : %s\n", meta.Name)
	fmt.Printf("Admin key  : %s\n", id.Fingerprint())
	fmt.Printf("Repository : %s\n", r.Path)
	fmt.Println("Initialized.")
	return nil
}
