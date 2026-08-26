package secret

import (
	"path/filepath"
	"testing"
)

func TestProtectorRoundTripAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "master.key")
	first, err := LoadOrCreate(path, "")
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, nonce, err := first.Encrypt("provider-secret", []byte("provider:test"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreate(path, "")
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := second.Decrypt(ciphertext, nonce, []byte("provider:test"))
	if err != nil || plaintext != "provider-secret" {
		t.Fatalf("unexpected round trip: %q %v", plaintext, err)
	}
	if _, err := second.Decrypt(ciphertext, nonce, []byte("provider:other")); err == nil {
		t.Fatal("associated data mismatch should fail")
	}
}
