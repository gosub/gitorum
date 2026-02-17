package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/gosub/gitorum/internal/api"
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
	// Best-effort; errors are non-fatal here.
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

	srv := api.New(servePort, absRepo)
	return srv.ListenAndServe(ui.StaticFS)
}
