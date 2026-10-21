package forum

import (
	"errors"
	"io/fs"
)

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

func exists(fsys fs.FS, name string) bool {
	_, err := fs.Stat(fsys, name)
	return err == nil
}
