package repo

import "fmt"

// The admin key is pinned in the repository's local git config (.git/config,
// section [gitorum]). That file is never pushed, so whoever controls the
// remote cannot replace the admin by editing GITORUM.toml.
const (
	pinSection = "gitorum"
	pinOption  = "adminkey"
)

// PinnedAdminKey returns the locally pinned admin public key, or "".
func (r *Repo) PinnedAdminKey() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, err := r.git.Config()
	if err != nil {
		return ""
	}
	return cfg.Raw.Section(pinSection).Option(pinOption)
}

// PinAdminKey records key as the trusted admin key for this repository.
func (r *Repo) PinAdminKey(key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pinAdminKey(key)
}

func (r *Repo) pinAdminKey(key string) error {
	cfg, err := r.git.Config()
	if err != nil {
		return fmt.Errorf("read git config: %w", err)
	}
	cfg.Raw.Section(pinSection).SetOption(pinOption, key)
	if err := r.git.SetConfig(cfg); err != nil {
		return fmt.Errorf("pin admin key: %w", err)
	}
	return nil
}
