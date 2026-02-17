package crypto

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
)

// Sign signs message with priv and returns a base64-encoded signature.
func Sign(priv ed25519.PrivateKey, message []byte) string {
	sig := ed25519.Sign(priv, message)
	return base64.StdEncoding.EncodeToString(sig)
}

// Verify checks a base64-encoded signature against pub and message.
// Returns nil on success, an error otherwise.
func Verify(pub ed25519.PublicKey, message []byte, sigB64 string) error {
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if !ed25519.Verify(pub, message, sig) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

// VerifyWithPublicKeyB64 is a convenience wrapper that decodes a base64 public
// key before verifying.
func VerifyWithPublicKeyB64(pubB64 string, message []byte, sigB64 string) error {
	b, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		return fmt.Errorf("decode public key: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return fmt.Errorf("public key: expected %d bytes, got %d", ed25519.PublicKeySize, len(b))
	}
	return Verify(ed25519.PublicKey(b), message, sigB64)
}

// CanonicalForm produces the canonical byte sequence used for signing a post.
//
// Format:
//
//	author=<value>\n
//	parent=<value>\n
//	pubkey=<value>\n
//	timestamp=<value>\n
//	\n
//	<body>
//
// Fields are sorted alphabetically (excluding "signature"). The body is
// appended verbatim after a blank line.
func CanonicalForm(fields map[string]string, body string) []byte {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if k != "signature" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(fields[k])
		sb.WriteByte('\n')
	}
	sb.WriteByte('\n')
	sb.WriteString(body)
	return []byte(sb.String())
}
