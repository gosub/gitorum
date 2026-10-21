package forum

import (
	"errors"
	"io/fs"
	"path"
)

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

// hiddenBy reports whether <dir>/<name> has a tombstone signed by adminKey
// that references content.
func hiddenBy(fsys fs.FS, dir, name string, content []byte, adminKey string) bool {
	tomb, err := fs.ReadFile(fsys, path.Join(dir, TombstoneFilename(name)))
	if err != nil {
		return false
	}
	return VerifyTombstone(tomb, content, adminKey) == nil
}

func exists(fsys fs.FS, name string) bool {
	_, err := fs.Stat(fsys, name)
	return err == nil
}
