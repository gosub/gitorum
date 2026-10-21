// Package forum implements the gitorum data model: parsing, signing, and
// verifying post files, and loading threads and categories from disk.
package forum

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gosub/gitorum/internal/crypto"
)

// SigStatus describes the signature validation state of a post.
type SigStatus int

const (
	SigValid     SigStatus = iota // present and verified
	SigInvalid                    // present but verification failed
	SigMissing                    // no public key found for this author
	SigUntrusted                  // valid signature, but the key is not certified by the admin
)

// rawFrontMatter holds the TOML-decoded fields from a post's +++ block.
type rawFrontMatter struct {
	Author    string `toml:"author"`
	PubKey    string `toml:"pubkey"`
	Timestamp string `toml:"timestamp"`
	Parent    string `toml:"parent"`
	Signature string `toml:"signature"`
}

// Post represents a parsed and optionally signature-verified post file.
type Post struct {
	// Front matter fields
	Author       string
	PubKey       string // ed25519 fingerprint (first 8 chars of base64 pubkey)
	Timestamp    time.Time
	TimestampRaw string // raw value from file, used verbatim in canonical form
	Parent       string // sha256 hex of parent file content; empty for root
	Signature    string // base64-encoded ed25519 signature

	// Content
	Body     string // raw Markdown body
	BodyHTML string // body rendered to HTML by goldmark

	// Metadata
	Filename   string    // e.g. "0000_root.md" or "1708123456789_a3f9c1b2.md"
	Hash       string    // PostHash of the file content; empty for unsaved posts
	SigStatus  SigStatus // set by VerifySignature
	SigError   string    // human-readable reason when SigStatus != SigValid
	Tombstoned bool      // true when a tombstone file exists; body/author are cleared
}

// ParsePost parses a .md file with TOML front matter fenced by +++.
// filename is stored in Post.Filename; content is the raw file bytes.
// SigStatus is left at its zero value (SigValid) – call VerifySignature to
// actually check the cryptographic signature.
func ParsePost(filename string, content []byte) (*Post, error) {
	fm, body, err := parseFrontMatter(content)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}
	ts, err := time.Parse(time.RFC3339, fm.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("parse timestamp in %s: %w", filename, err)
	}
	return &Post{
		Author:       fm.Author,
		PubKey:       fm.PubKey,
		Timestamp:    ts,
		TimestampRaw: fm.Timestamp,
		Parent:       fm.Parent,
		Signature:    fm.Signature,
		Body:         body,
		BodyHTML:     renderMarkdown(body),
		Filename:     filename,
		Hash:         PostHash(content),
	}, nil
}

// Verify checks the post signature against the author's keys in ring and
// sets SigStatus and SigError in place.
//
// A user may have several keys (after a rotation). The key whose fingerprint
// matches the post's pubkey field is tried first; the others are tried as a
// fallback so posts from before the fingerprint field mattered still verify.
func (p *Post) Verify(ring *KeyRing) {
	uk := ring.User(p.Author)
	if uk == nil || len(uk.Keys) == 0 {
		p.SigStatus = SigMissing
		p.SigError = fmt.Sprintf("no public key for author %q", p.Author)
		return
	}
	keys := make([]string, 0, len(uk.Keys))
	for _, k := range uk.Keys {
		if Fingerprint(k) == p.PubKey {
			keys = append([]string{k}, keys...)
		} else {
			keys = append(keys, k)
		}
	}
	canonical := crypto.CanonicalForm(p.fields(), p.Body)
	var lastErr error
	for _, k := range keys {
		if lastErr = crypto.VerifyWithPublicKeyB64(k, canonical, p.Signature); lastErr == nil {
			if !uk.Certified {
				p.SigStatus = SigUntrusted
				p.SigError = uk.Problem
				return
			}
			p.SigStatus = SigValid
			p.SigError = ""
			return
		}
	}
	p.SigStatus = SigInvalid
	p.SigError = lastErr.Error()
}

// Fingerprint returns the short key identifier stored in a post's pubkey
// field (the first 8 characters of the base64 public key).
func Fingerprint(pubkeyB64 string) string {
	if len(pubkeyB64) >= 8 {
		return pubkeyB64[:8]
	}
	return pubkeyB64
}

// VerifyTombstoneSignature checks only that tombContent is signed by
// adminKey. It is used for purged posts, whose content is no longer
// available to check the hash binding against.
func VerifyTombstoneSignature(tombContent []byte, adminKey string) error {
	tomb, err := ParsePost("tomb", tombContent)
	if err != nil {
		return err
	}
	canonical := crypto.CanonicalForm(tomb.fields(), tomb.Body)
	return crypto.VerifyWithPublicKeyB64(adminKey, canonical, tomb.Signature)
}

// VerifyTombstone reports whether tombContent is a valid tombstone for
// targetContent: signed by adminKey, with parent equal to the target's hash.
// The hash binding stops a tombstone from being copied onto another post.
func VerifyTombstone(tombContent, targetContent []byte, adminKey string) error {
	if adminKey == "" {
		return fmt.Errorf("no admin key pinned")
	}
	tomb, err := ParsePost("tomb", tombContent)
	if err != nil {
		return err
	}
	if tomb.Parent != PostHash(targetContent) {
		return fmt.Errorf("tombstone does not reference this post")
	}
	canonical := crypto.CanonicalForm(tomb.fields(), tomb.Body)
	if err := crypto.VerifyWithPublicKeyB64(adminKey, canonical, tomb.Signature); err != nil {
		return fmt.Errorf("tombstone not signed by the admin: %w", err)
	}
	return nil
}

// fields returns the signed front matter fields.
func (p *Post) fields() map[string]string {
	return map[string]string{
		"author":    p.Author,
		"pubkey":    p.PubKey,
		"timestamp": p.TimestampRaw,
		"parent":    p.Parent,
	}
}

// Format serializes the post to the on-disk file format:
//
//	+++
//	author    = "..."
//	pubkey    = "..."
//	timestamp = "..."
//	parent    = "..."
//	signature = "..."
//	+++
//
//	<body>
func (p *Post) Format() []byte {
	var sb strings.Builder
	sb.WriteString("+++\n")
	fmt.Fprintf(&sb, "author    = %q\n", p.Author)
	fmt.Fprintf(&sb, "pubkey    = %q\n", p.PubKey)
	fmt.Fprintf(&sb, "timestamp = %q\n", p.TimestampRaw)
	fmt.Fprintf(&sb, "parent    = %q\n", p.Parent)
	fmt.Fprintf(&sb, "signature = %q\n", p.Signature)
	sb.WriteString("+++\n\n")
	sb.WriteString(p.Body)
	return []byte(sb.String())
}

// PostHash returns the hex-encoded SHA-256 of the post file content.
// This is the value stored in the parent field of a direct reply.
func PostHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// SignPost creates a new post signed by identity.
// parent is PostHash of the parent file content, or "" for a root post.
// The caller must set Filename before writing to disk.
func SignPost(id *crypto.Identity, parent, body string) (*Post, error) {
	ts := time.Now().UTC()
	tsRaw := ts.Format(time.RFC3339)

	fields := map[string]string{
		"author":    id.Username,
		"pubkey":    id.Fingerprint(),
		"timestamp": tsRaw,
		"parent":    parent,
	}
	canonical := crypto.CanonicalForm(fields, body)

	priv, err := id.PrivKey()
	if err != nil {
		return nil, fmt.Errorf("get private key: %w", err)
	}
	sig := crypto.Sign(priv, canonical)

	return &Post{
		Author:       id.Username,
		PubKey:       id.Fingerprint(),
		Timestamp:    ts,
		TimestampRaw: tsRaw,
		Parent:       parent,
		Signature:    sig,
		Body:         body,
		BodyHTML:     renderMarkdown(body),
		SigStatus:    SigValid,
	}, nil
}

// TombstoneFilename returns the tombstone filename for a given post filename.
// e.g. "0000_root.md" → "0000_root.md.tomb"
func TombstoneFilename(postFilename string) string {
	return postFilename + ".tomb"
}

// SignTombstone creates a tombstone post signed by adminID.
// targetContent is the raw file bytes of the post being deleted; its hash is
// stored in the tombstone's parent field to create a cryptographic link.
func SignTombstone(adminID *crypto.Identity, targetContent []byte) (*Post, error) {
	return SignPost(adminID, PostHash(targetContent), "")
}

// ---- internal ----

// parseFrontMatter splits content into the TOML front matter and body.
// The file format is:
//
//	+++
//	<toml>
//	+++
//
//	<body>
//
// The blank line between the closing fence and the body is stripped.
func parseFrontMatter(content []byte) (fm rawFrontMatter, body string, err error) {
	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 || lines[0] != "+++" {
		return fm, "", fmt.Errorf("file does not begin with front matter fence '+++'")
	}

	closeAt := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "+++" {
			closeAt = i
			break
		}
	}
	if closeAt < 0 {
		return fm, "", fmt.Errorf("front matter closing '+++' not found")
	}

	tomlStr := strings.Join(lines[1:closeAt], "\n")
	if _, err = toml.Decode(tomlStr, &fm); err != nil {
		return fm, "", fmt.Errorf("decode TOML front matter: %w", err)
	}

	// Body: lines after the closing fence; strip one blank separator line.
	bodyLines := lines[closeAt+1:]
	if len(bodyLines) > 0 && bodyLines[0] == "" {
		bodyLines = bodyLines[1:]
	}
	body = strings.Join(bodyLines, "\n")
	// Trim a trailing empty element caused by a file-ending newline so that
	// round-tripping Format→ParsePost preserves the body exactly.
	body = strings.TrimSuffix(body, "\n")
	return fm, body, nil
}
