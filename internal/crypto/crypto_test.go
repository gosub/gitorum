package crypto_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gosub/gitorum/internal/crypto"
)

// ---- keygen / identity ----

func TestGenerate(t *testing.T) {
	id, err := crypto.Generate("alice")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if id.Username != "alice" {
		t.Errorf("username: got %q, want %q", id.Username, "alice")
	}
	if id.PublicKey == "" {
		t.Error("PublicKey is empty")
	}
	if id.PrivateKey == "" {
		t.Error("PrivateKey is empty")
	}
}

func TestIdentity_PubPrivKey(t *testing.T) {
	id, err := crypto.Generate("bob")
	if err != nil {
		t.Fatal(err)
	}
	pub, err := id.PubKey()
	if err != nil {
		t.Fatalf("PubKey: %v", err)
	}
	priv, err := id.PrivKey()
	if err != nil {
		t.Fatalf("PrivKey: %v", err)
	}
	if len(pub) != 32 {
		t.Errorf("pub key size: got %d, want 32", len(pub))
	}
	if len(priv) != 64 {
		t.Errorf("priv key size: got %d, want 64", len(priv))
	}
}

func TestIdentity_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.toml")

	orig, err := crypto.Generate("carol")
	if err != nil {
		t.Fatal(err)
	}
	if err := orig.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// File should be mode 0600
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode: got %o, want 0600", mode)
	}

	loaded, err := crypto.LoadIdentity(path)
	if err != nil {
		t.Fatalf("LoadIdentity: %v", err)
	}
	if loaded.Username != orig.Username {
		t.Errorf("username: got %q, want %q", loaded.Username, orig.Username)
	}
	if loaded.PublicKey != orig.PublicKey {
		t.Errorf("public key mismatch")
	}
	if loaded.PrivateKey != orig.PrivateKey {
		t.Errorf("private key mismatch")
	}
}

func TestLoadOrCreate_CreatesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "identity.toml")

	id, created, err := crypto.LoadOrCreate(path, "dave")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Error("expected created=true for new file")
	}
	if id.Username != "dave" {
		t.Errorf("username: got %q", id.Username)
	}

	// Second call should load existing.
	id2, created2, err := crypto.LoadOrCreate(path, "dave")
	if err != nil {
		t.Fatal(err)
	}
	if created2 {
		t.Error("expected created=false on second call")
	}
	if id2.PublicKey != id.PublicKey {
		t.Error("public key changed on reload")
	}
}

// ---- sign / verify ----

func TestSign_Verify_RoundTrip(t *testing.T) {
	id, err := crypto.Generate("eve")
	if err != nil {
		t.Fatal(err)
	}
	priv, err := id.PrivKey()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := id.PubKey()
	if err != nil {
		t.Fatal(err)
	}

	msg := []byte("hello gitorum")
	sig := crypto.Sign(priv, msg)
	if sig == "" {
		t.Fatal("Sign returned empty string")
	}
	if err := crypto.Verify(pub, msg, sig); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestVerify_InvalidSig(t *testing.T) {
	id, err := crypto.Generate("frank")
	if err != nil {
		t.Fatal(err)
	}
	pub, err := id.PubKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := crypto.Verify(pub, []byte("data"), "AAAA"); err == nil {
		t.Error("expected error for invalid signature")
	}
}

func TestVerify_WrongKey(t *testing.T) {
	id1, _ := crypto.Generate("user1")
	id2, _ := crypto.Generate("user2")
	priv1, _ := id1.PrivKey()
	pub2, _ := id2.PubKey()

	msg := []byte("message")
	sig := crypto.Sign(priv1, msg)
	if err := crypto.Verify(pub2, msg, sig); err == nil {
		t.Error("expected error when verifying with wrong public key")
	}
}

func TestVerify_TamperedMessage(t *testing.T) {
	id, _ := crypto.Generate("grace")
	priv, _ := id.PrivKey()
	pub, _ := id.PubKey()

	msg := []byte("original")
	sig := crypto.Sign(priv, msg)
	if err := crypto.Verify(pub, []byte("tampered"), sig); err == nil {
		t.Error("expected error for tampered message")
	}
}

// ---- canonical form ----

func TestCanonicalForm(t *testing.T) {
	tests := []struct {
		name   string
		fields map[string]string
		body   string
		want   string
	}{
		{
			name: "basic fields sorted",
			fields: map[string]string{
				"author":    "giampaolo",
				"pubkey":    "ABCD",
				"timestamp": "2026-02-17T10:00:00Z",
				"parent":    "",
			},
			body: "Hello world",
			want: "author=giampaolo\nparent=\npubkey=ABCD\ntimestamp=2026-02-17T10:00:00Z\n\nHello world",
		},
		{
			name: "signature field excluded",
			fields: map[string]string{
				"author":    "alice",
				"signature": "should-be-excluded",
				"pubkey":    "XYZ",
				"timestamp": "2026-01-01T00:00:00Z",
				"parent":    "abc123",
			},
			body: "Reply text",
			want: "author=alice\nparent=abc123\npubkey=XYZ\ntimestamp=2026-01-01T00:00:00Z\n\nReply text",
		},
		{
			name:   "empty body",
			fields: map[string]string{"author": "bob", "pubkey": "PQ", "timestamp": "T", "parent": ""},
			body:   "",
			want:   "author=bob\nparent=\npubkey=PQ\ntimestamp=T\n\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(crypto.CanonicalForm(tc.fields, tc.body))
			if got != tc.want {
				t.Errorf("CanonicalForm:\ngot:  %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// ---- full round-trip: sign canonical, verify ----

func TestSignCanonical_RoundTrip(t *testing.T) {
	id, _ := crypto.Generate("heidi")
	priv, _ := id.PrivKey()
	pub, _ := id.PubKey()

	fields := map[string]string{
		"author":    id.Username,
		"pubkey":    id.PublicKey,
		"timestamp": "2026-02-17T10:00:00Z",
		"parent":    "",
	}
	body := "My first post!"

	canonical := crypto.CanonicalForm(fields, body)
	sig := crypto.Sign(priv, canonical)

	// Add signature and re-derive canonical (sig should be excluded).
	fields["signature"] = sig
	canonical2 := crypto.CanonicalForm(fields, body)

	if err := crypto.Verify(pub, canonical2, sig); err != nil {
		t.Errorf("round-trip verify: %v", err)
	}
}

func TestVerifyWithPublicKeyB64(t *testing.T) {
	id, _ := crypto.Generate("ivan")
	priv, _ := id.PrivKey()

	msg := []byte("test message")
	sig := crypto.Sign(priv, msg)

	if err := crypto.VerifyWithPublicKeyB64(id.PublicKey, msg, sig); err != nil {
		t.Errorf("VerifyWithPublicKeyB64: %v", err)
	}

	// Tampered public key (wrong key)
	other, _ := crypto.Generate("other")
	if err := crypto.VerifyWithPublicKeyB64(other.PublicKey, msg, sig); err == nil {
		t.Error("expected error with wrong public key")
	}
}
