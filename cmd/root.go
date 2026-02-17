// Package cmd implements the gitorum command-line interface using Cobra.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gosub/gitorum/internal/crypto"
)

var rootCmd = &cobra.Command{
	Use:   "gitorum",
	Short: "Gitorum – a decentralized git-backed forum",
	Long: `Gitorum stores all forum content as signed files in a git repository
and distributes it via standard git transports (HTTP, SSH, email patches).`,
}

// Execute runs the root command and exits on error.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(keygenCmd)
}

// ---- keygen subcommand ----

var keygenUsername string
var keygenOutput string

var keygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Generate a new Ed25519 identity keypair",
	Long: `Generate a new Ed25519 identity keypair and store it in the identity file.
If the identity file already exists it will NOT be overwritten (use --force to override).`,
	RunE: runKeygen,
}

var keygenForce bool

func init() {
	keygenCmd.Flags().StringVarP(&keygenUsername, "username", "u", "", "username to associate with this identity (required)")
	keygenCmd.Flags().StringVarP(&keygenOutput, "output", "o", "", "path to write identity file (default: "+crypto.DefaultIdentityPath()+")")
	keygenCmd.Flags().BoolVar(&keygenForce, "force", false, "overwrite existing identity file")
	_ = keygenCmd.MarkFlagRequired("username")
}

func runKeygen(cmd *cobra.Command, args []string) error {
	path := keygenOutput
	if path == "" {
		path = crypto.DefaultIdentityPath()
	}

	// Check for existing file.
	if _, err := os.Stat(path); err == nil && !keygenForce {
		return fmt.Errorf("identity file already exists at %s (use --force to overwrite)", path)
	}

	id, err := crypto.Generate(keygenUsername)
	if err != nil {
		return fmt.Errorf("generate keypair: %w", err)
	}
	if err := id.Save(path); err != nil {
		return fmt.Errorf("save identity: %w", err)
	}

	fmt.Printf("Identity generated for %q\n", id.Username)
	fmt.Printf("Public key : %s\n", id.PublicKey)
	fmt.Printf("Fingerprint: %s\n", id.Fingerprint())
	fmt.Printf("Saved to   : %s\n", path)
	return nil
}
