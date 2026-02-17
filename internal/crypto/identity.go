// Package crypto handles Ed25519 key generation, signing, verification,
// and identity persistence for gitorum.
package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Identity holds the local user's Ed25519 keypair and metadata.
type Identity struct {
	Username   string `toml:"username"`
	PublicKey  string `toml:"public_key"`  // base64-encoded
	PrivateKey string `toml:"private_key"` // base64-encoded (full 64-byte seed+pub)
}

// identityFile is the on-disk representation.
type identityFile struct {
	Username   string `toml:"username"`
	PublicKey  string `toml:"public_key"`
	PrivateKey string `toml:"private_key"`
}

// Generate creates a new Ed25519 keypair for the given username.
func Generate(username string) (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("keygen: %w", err)
	}
	return &Identity{
		Username:   username,
		PublicKey:  base64.StdEncoding.EncodeToString(pub),
		PrivateKey: base64.StdEncoding.EncodeToString(priv),
	}, nil
}

// PrivKey decodes and returns the ed25519.PrivateKey.
func (id *Identity) PrivKey() (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(id.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(b) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key: expected %d bytes, got %d", ed25519.PrivateKeySize, len(b))
	}
	return ed25519.PrivateKey(b), nil
}

// PubKey decodes and returns the ed25519.PublicKey.
func (id *Identity) PubKey() (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(id.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key: expected %d bytes, got %d", ed25519.PublicKeySize, len(b))
	}
	return ed25519.PublicKey(b), nil
}

// Fingerprint returns a short human-readable identifier derived from the
// public key (first 8 bytes of base64, no padding concerns).
func (id *Identity) Fingerprint() string {
	if len(id.PublicKey) >= 8 {
		return id.PublicKey[:8]
	}
	return id.PublicKey
}

// DefaultIdentityPath returns the platform-appropriate path for the identity
// file, respecting XDG_CONFIG_HOME.
func DefaultIdentityPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			base = "."
		} else {
			base = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(base, "gitorum", "identity.toml")
}

// Save writes the identity to path, creating parent directories as needed.
// The file is written with mode 0600 to protect the private key.
func (id *Identity) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open identity file: %w", err)
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	return enc.Encode(identityFile{
		Username:   id.Username,
		PublicKey:  id.PublicKey,
		PrivateKey: id.PrivateKey,
	})
}

// LoadIdentity reads an identity from path.
func LoadIdentity(path string) (*Identity, error) {
	var f identityFile
	if _, err := toml.DecodeFile(path, &f); err != nil {
		return nil, fmt.Errorf("load identity: %w", err)
	}
	return &Identity{
		Username:   f.Username,
		PublicKey:  f.PublicKey,
		PrivateKey: f.PrivateKey,
	}, nil
}

// LoadOrCreate loads the identity from path; if the file does not exist it
// generates a new keypair for username and saves it before returning.
func LoadOrCreate(path, username string) (*Identity, bool, error) {
	if _, err := os.Stat(path); err == nil {
		// File exists – load it.
		id, err := LoadIdentity(path)
		if err != nil {
			return nil, false, err
		}
		return id, false, nil
	} else if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("stat identity file: %w", err)
	}
	// File does not exist – generate and save.
	id, err := Generate(username)
	if err != nil {
		return nil, false, err
	}
	if err := id.Save(path); err != nil {
		return nil, false, err
	}
	return id, true, nil
}
