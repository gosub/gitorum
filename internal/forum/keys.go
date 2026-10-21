package forum

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/gosub/gitorum/internal/crypto"
)

// KeysDir is the directory holding user public keys.
const KeysDir = "keys"

// KeyFile and CertFile return the paths of a user's key list and of the admin
// certificate (signature) over it.
func KeyFile(username string) string  { return path.Join(KeysDir, username+".pub") }
func CertFile(username string) string { return path.Join(KeysDir, username+".sig") }

// KeyCertMessage is the byte sequence the admin signs to certify that the
// key file content belongs to username.
func KeyCertMessage(username string, keyFile []byte) []byte {
	return []byte("gitorum-key-cert-v1\nusername=" + username + "\n\n" + string(keyFile))
}

// SignKeyFile returns the content of keys/<username>.sig for keyFile.
func SignKeyFile(admin *crypto.Identity, username string, keyFile []byte) ([]byte, error) {
	priv, err := admin.PrivKey()
	if err != nil {
		return nil, err
	}
	return []byte(crypto.Sign(priv, KeyCertMessage(username, keyFile)) + "\n"), nil
}

// EncodeKeyFile formats a list of public keys as a .pub file.
func EncodeKeyFile(keys []string) []byte {
	return []byte(strings.Join(keys, "\n") + "\n")
}

// UserKeys is one user's registered keys and whether the admin vouches for them.
type UserKeys struct {
	Keys      []string
	Certified bool   // signed by the pinned admin key, or the admin's own key
	Problem   string // why Certified is false, for diagnostics
}

// KeyRing maps usernames to their keys and certification status.
type KeyRing struct {
	AdminKey string // pinned admin public key; "" disables certification checks
	users    map[string]*UserKeys
}

// LoadKeyRing reads keys/<username>.pub (and .sig) from fsys. A key file is
// trusted when its .sig verifies under adminKey, or when it contains adminKey
// itself. Files with invalid usernames are ignored.
func LoadKeyRing(fsys fs.FS, adminKey string) (*KeyRing, error) {
	ring := &KeyRing{AdminKey: adminKey, users: map[string]*UserKeys{}}
	entries, err := fs.ReadDir(fsys, KeysDir)
	if err != nil {
		if isNotExist(err) {
			return ring, nil
		}
		return nil, fmt.Errorf("read keys dir: %w", err)
	}
	for _, e := range entries {
		user, ok := strings.CutSuffix(e.Name(), ".pub")
		if e.IsDir() || !ok || !ValidUsername(user) {
			continue
		}
		data, err := fs.ReadFile(fsys, KeyFile(user))
		if err != nil {
			return nil, fmt.Errorf("read key for %s: %w", user, err)
		}
		uk := &UserKeys{Keys: parseKeyLines(data)}
		uk.Certified, uk.Problem = certify(fsys, user, data, uk.Keys, adminKey)
		ring.users[user] = uk
	}
	return ring, nil
}

func certify(fsys fs.FS, user string, data []byte, keys []string, adminKey string) (bool, string) {
	if adminKey == "" {
		return false, "no admin key pinned"
	}
	for _, k := range keys {
		if k == adminKey {
			return true, ""
		}
	}
	sig, err := fs.ReadFile(fsys, CertFile(user))
	if err != nil {
		return false, "key not certified by the admin (no " + CertFile(user) + ")"
	}
	msg := KeyCertMessage(user, data)
	if err := crypto.VerifyWithPublicKeyB64(adminKey, msg, strings.TrimSpace(string(sig))); err != nil {
		return false, "admin certificate does not match " + KeyFile(user)
	}
	return true, ""
}

// User returns username's keys, or nil.
func (k *KeyRing) User(username string) *UserKeys { return k.users[username] }

// Keys returns the public keys registered for username.
func (k *KeyRing) Keys(username string) []string {
	if u := k.users[username]; u != nil {
		return u.Keys
	}
	return nil
}

// Has reports whether username has at least one key.
func (k *KeyRing) Has(username string) bool { return len(k.Keys(username)) > 0 }

// Users returns all usernames with a key file.
func (k *KeyRing) Users() []string {
	out := make([]string, 0, len(k.users))
	for u := range k.users {
		out = append(out, u)
	}
	return out
}

// parseKeyLines returns the non-empty, non-comment lines of a .pub file.
func parseKeyLines(data []byte) []string {
	var keys []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keys = append(keys, line)
	}
	return keys
}
