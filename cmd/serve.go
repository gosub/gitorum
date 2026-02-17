package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/gosub/gitorum/internal/api"
	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/repo"
	"github.com/gosub/gitorum/internal/ui"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Gitorum web server",
	Long: `Start the local HTTP server that serves the Gitorum web UI and JSON API.

All forum data is read from and written to the local git repository at --repo.
Open http://localhost:<port> in your browser to use the forum.`,
	RunE: runServe,
}

var (
	servePort     int
	serveRepoPath string
	serveIdentity string
)

func init() {
	serveCmd.Flags().IntVarP(&servePort, "port", "p", 8080, "HTTP port to listen on")
	serveCmd.Flags().StringVar(&serveRepoPath, "repo", ".", "path to the forum git repository")
	serveCmd.Flags().StringVar(&serveIdentity, "identity", "", "path to identity file (default: "+defaultIdentityHint()+")")

	rootCmd.AddCommand(serveCmd)
}

func defaultIdentityHint() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "$XDG_CONFIG_HOME/gitorum/identity.toml"
	}
	return filepath.Join(home, ".config", "gitorum", "identity.toml")
}

func runServe(cmd *cobra.Command, args []string) error {
	absRepo, err := filepath.Abs(serveRepoPath)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}

	identPath := serveIdentity
	if identPath == "" {
		identPath = crypto.DefaultIdentityPath()
	}

	// Load identity — non-fatal if missing (setup wizard will handle it).
	var id *crypto.Identity
	if loaded, err := crypto.LoadIdentity(identPath); err == nil {
		id = loaded
		log.Printf("Identity: @%s", id.Username)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("load identity: %w", err)
	} else {
		log.Printf("No identity file found at %s; run 'gitorum keygen' first", identPath)
	}

	// Open forum repo — non-fatal if missing (setup wizard will handle it).
	var r *repo.Repo
	if opened, err := repo.Open(absRepo); err == nil {
		r = opened
		log.Printf("Forum repo: %s", absRepo)
	} else {
		log.Printf("No forum repo at %s; run 'gitorum init' first", absRepo)
	}

	srv := api.New(servePort, absRepo, r, id)
	return srv.ListenAndServe(ui.StaticFS)
}
