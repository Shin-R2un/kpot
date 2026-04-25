package crypto

import (
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

var ErrAuthFailed = errors.New("decryption failed (wrong passphrase or corrupted data)")

func NewNonce() ([]byte, error) {
	n := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(n); err != nil {
		return nil, fmt.Errorf("nonce generation: %w", err)
	}
	return n, nil
}

func Seal(key, nonce, plaintext, aad []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("key must be %d bytes", KeySize)
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("aead init: %w", err)
	}
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("nonce must be %d bytes", aead.NonceSize())
	}
	return aead.Seal(nil, nonce, plaintext, aad), nil
}

func Open(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("key must be %d bytes", KeySize)
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("aead init: %w", err)
	}
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("nonce must be %d bytes", aead.NonceSize())
	}
	pt, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrAuthFailed
	}
	return pt, nil
}
