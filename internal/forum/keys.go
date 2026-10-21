package forum

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// KeysDir is the directory holding user public keys.
const KeysDir = "keys"

// KeyRing maps usernames to the public keys found in keys/<username>.pub.
type KeyRing struct {
	users map[string][]string
}

// LoadKeyRing reads every keys/<username>.pub file from fsys.
// Files with invalid usernames are ignored.
func LoadKeyRing(fsys fs.FS) (*KeyRing, error) {
	ring := &KeyRing{users: map[string][]string{}}
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
		data, err := fs.ReadFile(fsys, path.Join(KeysDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read key for %s: %w", user, err)
		}
		ring.users[user] = parseKeyLines(data)
	}
	return ring, nil
}

// Keys returns the public keys registered for username.
func (k *KeyRing) Keys(username string) []string { return k.users[username] }

// Has reports whether username has at least one key.
func (k *KeyRing) Has(username string) bool { return len(k.users[username]) > 0 }

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
