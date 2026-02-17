package forum

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

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

// LoadCategory reads META.toml from dir and enumerates valid thread
// subdirectories (those that contain a 0000_root.md file).
func LoadCategory(slug, dir string) (*Category, error) {
	var meta categoryMeta
	if _, err := toml.DecodeFile(filepath.Join(dir, "META.toml"), &meta); err != nil {
		return nil, fmt.Errorf("read META.toml for category %q: %w", slug, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read category dir %q: %w", slug, err)
	}

	c := &Category{
		Slug:        slug,
		Name:        meta.Name,
		Description: meta.Description,
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		rootPath := filepath.Join(dir, entry.Name(), RootFilename)
		if _, err := os.Stat(rootPath); err == nil {
			c.ThreadSlugs = append(c.ThreadSlugs, entry.Name())
		}
	}
	sort.Strings(c.ThreadSlugs)
	return c, nil
}
