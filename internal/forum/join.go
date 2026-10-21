package forum

import (
	"fmt"
	"strings"

	"github.com/gosub/gitorum/internal/crypto"
)

// A join request proves possession of the private key: it carries the
// requester's signature over JoinRequestMessage. Without that, anyone could
// file a request in someone else's name with someone else's key.
//
// It travels either as requests/<username>.pub in the repository (two lines:
// public key, signature) or as a one-line token sent to the admin out of
// band, for people who cannot push to the forum remote.

const joinTokenPrefix = "gitorum-join:"

// JoinRequestMessage is the byte sequence signed by a join request.
func JoinRequestMessage(username, pubkey string) []byte {
	return []byte("gitorum-join-v1\nusername=" + username + "\npubkey=" + pubkey + "\n")
}

// SignJoinRequest returns the requests/<username>.pub content and the
// equivalent out-of-band token for id.
func SignJoinRequest(id *crypto.Identity) (file []byte, token string, err error) {
	priv, err := id.PrivKey()
	if err != nil {
		return nil, "", err
	}
	sig := crypto.Sign(priv, JoinRequestMessage(id.Username, id.PublicKey))
	file = []byte(id.PublicKey + "\n" + sig + "\n")
	token = joinTokenPrefix + id.Username + ":" + id.PublicKey + ":" + sig
	return file, token, nil
}

// VerifyJoinRequest parses a requests/<username>.pub file. It returns the
// public key and nil when the proof of possession is valid.
func VerifyJoinRequest(username string, file []byte) (pubkey string, err error) {
	lines := parseKeyLines(file)
	if len(lines) == 0 {
		return "", fmt.Errorf("empty request")
	}
	pubkey = lines[0]
	if len(lines) < 2 {
		return pubkey, fmt.Errorf("request is not signed by its key")
	}
	if err := crypto.VerifyWithPublicKeyB64(pubkey, JoinRequestMessage(username, pubkey), lines[1]); err != nil {
		return pubkey, fmt.Errorf("request signature: %w", err)
	}
	return pubkey, nil
}

// ParseJoinToken verifies an out-of-band join token and returns its
// username and public key.
func ParseJoinToken(token string) (username, pubkey string, err error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(token), joinTokenPrefix)
	if !ok {
		return "", "", fmt.Errorf("not a gitorum join token")
	}
	parts := strings.Split(rest, ":")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("malformed join token")
	}
	username, pubkey = parts[0], parts[1]
	if !ValidUsername(username) {
		return "", "", fmt.Errorf("invalid username %q in token", username)
	}
	if err := crypto.VerifyWithPublicKeyB64(pubkey, JoinRequestMessage(username, pubkey), parts[2]); err != nil {
		return "", "", fmt.Errorf("join token signature: %w", err)
	}
	return username, pubkey, nil
}
