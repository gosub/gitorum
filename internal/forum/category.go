package forum

import (
	"fmt"
	"io/fs"
	"path"
	"sort"

	"github.com/BurntSushi/toml"
)

// CategoryMetaFile is the per-category metadata file.
const CategoryMetaFile = "META.toml"

// categoryMeta is the raw TOML structure of a META.toml file.
type categoryMeta struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
}

// Category represents a forum category.
type Category struct {
	Slug        string
	Name        string
	Description string
	ThreadSlugs []string // slugs of valid threads (have 0000_root.md), sorted
}

// EncodeCategoryMeta returns the META.toml content for a category.
func EncodeCategoryMeta(name, description string) []byte {
	return []byte(fmt.Sprintf("name = %q\ndescription = %q\n", name, description))
}

// ListCategories returns the slugs of all categories in fsys: top-level
// directories with a valid slug that contain META.toml, sorted.
func ListCategories(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read repo root: %w", err)
	}
	var cats []string
	for _, e := range entries {
		if e.IsDir() && ValidSlug(e.Name()) && exists(fsys, path.Join(e.Name(), CategoryMetaFile)) {
			cats = append(cats, e.Name())
		}
	}
	sort.Strings(cats)
	return cats, nil
}

// LoadCategory reads <slug>/META.toml from fsys and enumerates valid thread
// subdirectories (those that contain a 0000_root.md file).
func LoadCategory(fsys fs.FS, slug string) (*Category, error) {
	if !ValidSlug(slug) {
		return nil, fmt.Errorf("invalid category slug %q: %w", slug, fs.ErrNotExist)
	}
	data, err := fs.ReadFile(fsys, path.Join(slug, CategoryMetaFile))
	if err != nil {
		return nil, fmt.Errorf("read META.toml for category %q: %w", slug, err)
	}
	var meta categoryMeta
	if _, err := toml.Decode(string(data), &meta); err != nil {
		return nil, fmt.Errorf("decode META.toml for category %q: %w", slug, err)
	}

	entries, err := fs.ReadDir(fsys, slug)
	if err != nil {
		return nil, fmt.Errorf("read category dir %q: %w", slug, err)
	}

	c := &Category{
		Slug:        slug,
		Name:        meta.Name,
		Description: meta.Description,
	}
	for _, entry := range entries {
		if !entry.IsDir() || !ValidSlug(entry.Name()) {
			continue
		}
		if exists(fsys, path.Join(slug, entry.Name(), RootFilename)) {
			c.ThreadSlugs = append(c.ThreadSlugs, entry.Name())
		}
	}
	sort.Strings(c.ThreadSlugs)
	return c, nil
}
