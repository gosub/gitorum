package repo

import (
	"errors"
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// TreeFS is a read-only fs.FS over a git tree. It implements fs.ReadDirFS,
// fs.ReadFileFS and fs.StatFS. Object access is serialized through the
// owning Repo's mutex, so a TreeFS may be used concurrently with commits.
type TreeFS struct {
	r    *Repo
	tree *object.Tree // nil for an empty repository
}

var (
	_ fs.ReadDirFS  = (*TreeFS)(nil)
	_ fs.ReadFileFS = (*TreeFS)(nil)
	_ fs.StatFS     = (*TreeFS)(nil)
)

// Open opens the named file or directory.
func (t *TreeFS) Open(name string) (fs.File, error) {
	info, err := t.Stat(name)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		entries, err := t.ReadDir(name)
		if err != nil {
			return nil, err
		}
		return &treeDir{info: info, entries: entries}, nil
	}
	data, err := t.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return &treeFile{info: info, r: strings.NewReader(string(data))}, nil
}

// ReadFile returns the contents of the named blob.
func (t *TreeFS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrInvalid}
	}
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	if t.tree == nil {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	f, err := t.tree.File(name)
	if err != nil {
		return nil, &fs.PathError{Op: "read", Path: name, Err: notExist(err)}
	}
	s, err := f.Contents()
	if err != nil {
		return nil, &fs.PathError{Op: "read", Path: name, Err: err}
	}
	return []byte(s), nil
}

// ReadDir lists the named directory, sorted by name.
func (t *TreeFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	if t.tree == nil {
		if name == "." {
			return nil, nil
		}
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	dir := t.tree
	if name != "." {
		sub, err := t.tree.Tree(name)
		if err != nil {
			return nil, &fs.PathError{Op: "readdir", Path: name, Err: notExist(err)}
		}
		dir = sub
	}
	out := make([]fs.DirEntry, 0, len(dir.Entries))
	for _, e := range dir.Entries {
		out = append(out, fileInfo{name: e.Name, dir: e.Mode == filemode.Dir})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// Stat returns file info for the named path.
func (t *TreeFS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	if name == "." {
		return fileInfo{name: ".", dir: true}, nil
	}
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	if t.tree == nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	e, err := t.tree.FindEntry(name)
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: notExist(err)}
	}
	base := name[strings.LastIndexByte(name, '/')+1:]
	return fileInfo{name: base, dir: e.Mode == filemode.Dir}, nil
}

func notExist(err error) error {
	if errors.Is(err, object.ErrFileNotFound) ||
		errors.Is(err, object.ErrDirectoryNotFound) ||
		errors.Is(err, object.ErrEntryNotFound) {
		return fs.ErrNotExist
	}
	return err
}

// fileInfo implements both fs.FileInfo and fs.DirEntry. Sizes and times are
// not tracked: git trees carry neither cheaply.
type fileInfo struct {
	name string
	dir  bool
}

func (f fileInfo) Name() string { return f.name }
func (f fileInfo) Size() int64  { return 0 }
func (f fileInfo) Mode() fs.FileMode {
	if f.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (f fileInfo) ModTime() time.Time         { return time.Time{} }
func (f fileInfo) IsDir() bool                { return f.dir }
func (f fileInfo) Sys() any                   { return nil }
func (f fileInfo) Type() fs.FileMode          { return f.Mode().Type() }
func (f fileInfo) Info() (fs.FileInfo, error) { return f, nil }

type treeFile struct {
	info fs.FileInfo
	r    io.Reader
}

func (f *treeFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *treeFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *treeFile) Close() error               { return nil }

type treeDir struct {
	info    fs.FileInfo
	entries []fs.DirEntry
}

func (d *treeDir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *treeDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.Name(), Err: fs.ErrInvalid}
}
func (d *treeDir) Close() error { return nil }
func (d *treeDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if n <= 0 {
		out := d.entries
		d.entries = nil
		return out, nil
	}
	if len(d.entries) == 0 {
		return nil, io.EOF
	}
	if n > len(d.entries) {
		n = len(d.entries)
	}
	out := d.entries[:n]
	d.entries = d.entries[n:]
	return out, nil
}
