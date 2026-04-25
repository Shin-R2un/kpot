package vault

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/r2un/kpot/internal/crypto"
)

const (
	FormatName    = "kpot"
	FormatVersion = 1
)

type KDFSection struct {
	Name   string                `json:"name"`
	Salt   string                `json:"salt"`
	Params crypto.Argon2idParams `json:"params"`
}

type CipherSection struct {
	Name  string `json:"name"`
	Nonce string `json:"nonce"`
}

// Header is the unencrypted envelope of a .kpot file.
// The canonical JSON form (without the payload field) is bound as AAD
// to the AEAD ciphertext, so any tampering with the header parameters
// (e.g. KDF downgrade) is detected on decryption.
type Header struct {
	Format  string        `json:"format"`
	Version int           `json:"version"`
	KDF     KDFSection    `json:"kdf"`
	Cipher  CipherSection `json:"cipher"`
	Payload string        `json:"payload"`
}

func (h *Header) Validate() error {
	if h.Format != FormatName {
		return fmt.Errorf("not a kpot vault file (format=%q)", h.Format)
	}
	if h.Version > FormatVersion {
		return fmt.Errorf("vault format v%d is newer than supported v%d", h.Version, FormatVersion)
	}
	if h.Version < 1 {
		return fmt.Errorf("invalid vault version: %d", h.Version)
	}
	if h.KDF.Name != "argon2id" {
		return fmt.Errorf("unsupported KDF: %s", h.KDF.Name)
	}
	if h.Cipher.Name != "xchacha20-poly1305" {
		return fmt.Errorf("unsupported cipher: %s", h.Cipher.Name)
	}
	if err := h.KDF.Params.Validate(); err != nil {
		return err
	}
	return nil
}

// AAD returns the canonical bytes used as Additional Authenticated Data.
// Stable across encrypt and decrypt — must not include the payload.
func (h *Header) AAD() ([]byte, error) {
	type aadDoc struct {
		Format  string        `json:"format"`
		Version int           `json:"version"`
		KDF     KDFSection    `json:"kdf"`
		Cipher  CipherSection `json:"cipher"`
	}
	doc := aadDoc{
		Format:  h.Format,
		Version: h.Version,
		KDF:     h.KDF,
		Cipher:  h.Cipher,
	}
	return json.Marshal(doc)
}

func (h *Header) DecodeSalt() ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(h.KDF.Salt)
	if err != nil {
		return nil, fmt.Errorf("invalid salt encoding: %w", err)
	}
	return b, nil
}

func (h *Header) DecodeNonce() ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(h.Cipher.Nonce)
	if err != nil {
		return nil, fmt.Errorf("invalid nonce encoding: %w", err)
	}
	return b, nil
}

func (h *Header) DecodePayload() ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(h.Payload)
	if err != nil {
		return nil, fmt.Errorf("invalid payload encoding: %w", err)
	}
	return b, nil
}
