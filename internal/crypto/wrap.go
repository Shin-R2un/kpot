package crypto

import (
	"crypto/rand"
	"fmt"
)

// DEKSize is the byte length of a Data Encryption Key. Matches KeySize
// because the DEK is itself used as a XChaCha20-Poly1305 key for the
// vault payload.
const DEKSize = KeySize

// NewDEK returns a fresh random Data Encryption Key. The vault payload
// is encrypted with this key; key wrapping (Wrap/Unwrap) lets the same
// DEK be unlocked from multiple recovery paths (passphrase, seed, etc.)
// without re-encrypting the payload.
func NewDEK() ([]byte, error) {
	d := make([]byte, DEKSize)
	if _, err := rand.Read(d); err != nil {
		return nil, fmt.Errorf("dek generation: %w", err)
	}
	return d, nil
}

// Wrap encrypts dek using kek (a 32-byte Key Encryption Key) under
// XChaCha20-Poly1305 with the supplied nonce and AAD. The returned
// bytes include the 16-byte Poly1305 tag, so the caller stores it as a
// single blob. Caller is responsible for nonce uniqueness per kek and
// for choosing AAD that binds the wrap to its surrounding header
// fields (wrap type, KDF params) so they can't be silently re-pointed.
func Wrap(kek, nonce, dek, aad []byte) ([]byte, error) {
	if len(dek) != DEKSize {
		return nil, fmt.Errorf("dek must be %d bytes, got %d", DEKSize, len(dek))
	}
	return Seal(kek, nonce, dek, aad)
}

// Unwrap reverses Wrap. A wrong kek (or any tamper) yields ErrAuthFailed
// — same sentinel as a passphrase mismatch on the outer payload, so
// the caller can surface a single uniform "wrong passphrase or
// corrupted file" message regardless of which step rejected.
func Unwrap(kek, nonce, wrapped, aad []byte) ([]byte, error) {
	dek, err := Open(kek, nonce, wrapped, aad)
	if err != nil {
		return nil, err
	}
	if len(dek) != DEKSize {
		return nil, fmt.Errorf("unwrapped dek has wrong length: %d", len(dek))
	}
	return dek, nil
}
