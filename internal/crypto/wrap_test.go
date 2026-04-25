package crypto

import (
	"bytes"
	"errors"
	"testing"
)

func TestWrapUnwrapRoundTrip(t *testing.T) {
	kek := bytes.Repeat([]byte{0xAA}, KeySize)
	dek, err := NewDEK()
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	aad := []byte(`{"type":"passphrase"}`)

	wrapped, err := Wrap(kek, nonce, dek, aad)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unwrap(kek, nonce, wrapped, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, dek) {
		t.Fatalf("dek mismatch")
	}
}

func TestUnwrapWrongKEK(t *testing.T) {
	kek := bytes.Repeat([]byte{0xAA}, KeySize)
	wrong := bytes.Repeat([]byte{0xBB}, KeySize)
	dek, _ := NewDEK()
	nonce, _ := NewNonce()

	wrapped, _ := Wrap(kek, nonce, dek, nil)
	if _, err := Unwrap(wrong, nonce, wrapped, nil); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got %v", err)
	}
}

func TestUnwrapWrongAAD(t *testing.T) {
	kek := bytes.Repeat([]byte{0xAA}, KeySize)
	dek, _ := NewDEK()
	nonce, _ := NewNonce()

	wrapped, _ := Wrap(kek, nonce, dek, []byte("aad-original"))
	if _, err := Unwrap(kek, nonce, wrapped, []byte("aad-tampered")); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("AAD swap should authfail, got %v", err)
	}
}

func TestWrapRejectsBadDEKSize(t *testing.T) {
	kek := bytes.Repeat([]byte{0xAA}, KeySize)
	nonce, _ := NewNonce()
	if _, err := Wrap(kek, nonce, []byte("too-short"), nil); err == nil {
		t.Fatal("expected error for short dek")
	}
}

func TestNewDEKIsRandom(t *testing.T) {
	a, _ := NewDEK()
	b, _ := NewDEK()
	if bytes.Equal(a, b) {
		t.Fatal("two NewDEK calls returned identical bytes")
	}
	if len(a) != DEKSize {
		t.Fatalf("DEK length = %d, want %d", len(a), DEKSize)
	}
}
