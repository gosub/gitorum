package forum

import "regexp"

// Every name that comes from a request or from a (possibly hostile) repository
// and ends up in a filesystem path or in the UI must pass one of these checks.
// They exclude path separators, "..", whitespace and quote characters.
var (
	slugRe         = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	usernameRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	postFilenameRe = regexp.MustCompile(`^(0000_root|[0-9]{1,20}_[0-9a-f]{8,64})\.md$`)
)

// ValidSlug reports whether s is a valid category or thread slug.
func ValidSlug(s string) bool { return slugRe.MatchString(s) }

// ValidUsername reports whether s is a valid username.
func ValidUsername(s string) bool { return usernameRe.MatchString(s) }

// ValidPostFilename reports whether s is a well-formed post filename
// ("0000_root.md" or "{unix_millis}_{hex}.md").
func ValidPostFilename(s string) bool { return postFilenameRe.MatchString(s) }
