package crypto

import (
	"bytes"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	pass := []byte("correct horse battery staple")
	salt, err := NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	params := DefaultArgon2idParams()
	key := DeriveKey(pass, salt, params)
	if len(key) != KeySize {
		t.Fatalf("key size = %d, want %d", len(key), KeySize)
	}

	nonce, err := NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte(`{"hello":"world"}`)
	aad := []byte(`{"format":"kpot","version":1}`)

	ct, err := Seal(key, nonce, plaintext, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, plaintext) {
		t.Fatal("ciphertext contains plaintext bytes")
	}

	pt, err := Open(key, nonce, ct, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatalf("round trip mismatch: %q vs %q", pt, plaintext)
	}
}

func TestWrongPassphrase(t *testing.T) {
	salt, _ := NewSalt()
	params := DefaultArgon2idParams()
	key1 := DeriveKey([]byte("right"), salt, params)
	key2 := DeriveKey([]byte("wrong"), salt, params)
	nonce, _ := NewNonce()
	ct, _ := Seal(key1, nonce, []byte("secret"), nil)

	if _, err := Open(key2, nonce, ct, nil); err != ErrAuthFailed {
		t.Fatalf("expected ErrAuthFailed, got %v", err)
	}
}

func TestAADTampering(t *testing.T) {
	salt, _ := NewSalt()
	key := DeriveKey([]byte("p"), salt, DefaultArgon2idParams())
	nonce, _ := NewNonce()
	ct, _ := Seal(key, nonce, []byte("secret"), []byte("aad-original"))

	if _, err := Open(key, nonce, ct, []byte("aad-tampered")); err != ErrAuthFailed {
		t.Fatalf("expected ErrAuthFailed for tampered AAD, got %v", err)
	}
}

func TestCiphertextTampering(t *testing.T) {
	salt, _ := NewSalt()
	key := DeriveKey([]byte("p"), salt, DefaultArgon2idParams())
	nonce, _ := NewNonce()
	ct, _ := Seal(key, nonce, []byte("secret"), nil)

	ct[0] ^= 0xff
	if _, err := Open(key, nonce, ct, nil); err != ErrAuthFailed {
		t.Fatalf("expected ErrAuthFailed for tampered ciphertext, got %v", err)
	}
}

func TestZero(t *testing.T) {
	b := []byte{1, 2, 3, 4, 5}
	Zero(b)
	for i, v := range b {
		if v != 0 {
			t.Fatalf("byte %d = %d, want 0", i, v)
		}
	}
}
