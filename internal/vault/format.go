package vault

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/Shin-R2un/kpot/internal/crypto"
)

const (
	FormatName    = "kpot"
	FormatVersion = 2 // bumped for the recovery_wrap envelope (v0.3+)

	// WrapKindPassphrase / WrapKindSeed / WrapKindKey are the legal
	// values for Wrap.Kind. Stored verbatim in the JSON header so a
	// v2 reader can dispatch the right derivation.
	WrapKindPassphrase = "passphrase"
	WrapKindSeed       = "seed-bip39"
	WrapKindSecretKey  = "secret-key"

	KDFArgon2id     = "argon2id"
	KDFPBKDF2SHA512 = "pbkdf2-sha512"
	KDFHKDFSHA256   = "hkdf-sha256"
)

// KDFSection captures Argon2id parameters for a v1 vault header. v2
// vaults move this into a per-wrap WrapKDF and leave the top-level
// KDF nil.
type KDFSection struct {
	Name   string                `json:"name"`
	Salt   string                `json:"salt"`
	Params crypto.Argon2idParams `json:"params"`
}

type CipherSection struct {
	Name  string `json:"name"`
	Nonce string `json:"nonce"`
}

// WrapKDF describes how a wrap derives its KEK. Different KDF families
// use different fields — only the ones relevant to Name need to be set.
type WrapKDF struct {
	Name       string                 `json:"name"`
	Salt       string                 `json:"salt,omitempty"`       // argon2id, pbkdf2
	Params     *crypto.Argon2idParams `json:"params,omitempty"`     // argon2id only
	Iterations uint32                 `json:"iterations,omitempty"` // pbkdf2 only
}

// Wrap is one independent path that can unlock the vault's DEK. v2
// vaults always have a passphrase wrap, optionally a recovery wrap.
type Wrap struct {
	Kind       string  `json:"kind"`
	KDF        WrapKDF `json:"kdf"`
	Nonce      string  `json:"nonce"`
	WrappedDEK string  `json:"wrapped_dek"`
}

// Header is the unencrypted envelope of a .kpot file. The shape is
// version-aware: v1 fills KDF and uses the payload key directly; v2
// fills PassphraseWrap (and optionally RecoveryWrap), each of which
// wraps a separate DEK that encrypts the payload.
//
// Canonical AAD-bound representation is computed in AAD(); any header
// tampering (KDF downgrade, wrap swap, KEK confusion) breaks
// authentication on decrypt.
type Header struct {
	Format         string        `json:"format"`
	Version        int           `json:"version"`
	KDF            *KDFSection   `json:"kdf,omitempty"`             // v1 only
	PassphraseWrap *Wrap         `json:"passphrase_wrap,omitempty"` // v2 only
	RecoveryWrap   *Wrap         `json:"recovery_wrap,omitempty"`   // v2 only, optional
	Cipher         CipherSection `json:"cipher"`
	Payload        string        `json:"payload"`
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
	if h.Cipher.Name != "xchacha20-poly1305" {
		return fmt.Errorf("unsupported cipher: %s", h.Cipher.Name)
	}
	switch h.Version {
	case 1:
		if h.KDF == nil {
			return fmt.Errorf("v1 vault is missing kdf section")
		}
		if h.KDF.Name != KDFArgon2id {
			return fmt.Errorf("unsupported KDF: %s", h.KDF.Name)
		}
		return h.KDF.Params.Validate()
	case 2:
		if h.PassphraseWrap == nil {
			return fmt.Errorf("v2 vault is missing passphrase_wrap")
		}
		if err := h.PassphraseWrap.validate(); err != nil {
			return fmt.Errorf("passphrase_wrap: %w", err)
		}
		if h.RecoveryWrap != nil {
			if err := h.RecoveryWrap.validate(); err != nil {
				return fmt.Errorf("recovery_wrap: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("unhandled version %d", h.Version)
}

func (w *Wrap) validate() error {
	switch w.Kind {
	case WrapKindPassphrase:
		if w.KDF.Name != KDFArgon2id {
			return fmt.Errorf("passphrase wrap requires %s, got %s", KDFArgon2id, w.KDF.Name)
		}
		if w.KDF.Params == nil {
			return fmt.Errorf("passphrase wrap missing argon2id params")
		}
		if err := w.KDF.Params.Validate(); err != nil {
			return err
		}
	case WrapKindSeed:
		if w.KDF.Name != KDFPBKDF2SHA512 {
			return fmt.Errorf("seed wrap requires %s, got %s", KDFPBKDF2SHA512, w.KDF.Name)
		}
		if w.KDF.Iterations == 0 {
			return fmt.Errorf("seed wrap missing iterations")
		}
	case WrapKindSecretKey:
		if w.KDF.Name != KDFHKDFSHA256 {
			return fmt.Errorf("secret-key wrap requires %s, got %s", KDFHKDFSHA256, w.KDF.Name)
		}
	default:
		return fmt.Errorf("unknown wrap kind: %s", w.Kind)
	}
	if w.Nonce == "" || w.WrappedDEK == "" {
		return fmt.Errorf("%s wrap missing nonce or wrapped_dek", w.Kind)
	}
	return nil
}

// AAD returns the canonical bytes used as Additional Authenticated Data
// for the payload AEAD. Stable across encrypt/decrypt; never includes
// the payload itself. v2 binds both wraps so a swap or removal is
// detected on decrypt.
func (h *Header) AAD() ([]byte, error) {
	switch h.Version {
	case 1:
		type aadV1 struct {
			Format  string        `json:"format"`
			Version int           `json:"version"`
			KDF     KDFSection    `json:"kdf"`
			Cipher  CipherSection `json:"cipher"`
		}
		return json.Marshal(aadV1{Format: h.Format, Version: h.Version, KDF: *h.KDF, Cipher: h.Cipher})
	case 2:
		type aadV2 struct {
			Format         string        `json:"format"`
			Version        int           `json:"version"`
			PassphraseWrap Wrap          `json:"passphrase_wrap"`
			RecoveryWrap   *Wrap         `json:"recovery_wrap,omitempty"`
			Cipher         CipherSection `json:"cipher"`
		}
		return json.Marshal(aadV2{
			Format:         h.Format,
			Version:        h.Version,
			PassphraseWrap: *h.PassphraseWrap,
			RecoveryWrap:   h.RecoveryWrap,
			Cipher:         h.Cipher,
		})
	}
	return nil, fmt.Errorf("AAD: unhandled version %d", h.Version)
}

// WrapAAD binds a wrap to its surrounding header context so an attacker
// can't move a wrap blob between vaults or swap kinds without breaking
// authentication on unwrap.
func (h *Header) WrapAAD(w *Wrap) ([]byte, error) {
	type wrapAAD struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
		Wrap    Wrap   `json:"wrap"`
	}
	wCopy := *w
	wCopy.WrappedDEK = "" // bind everything except the ciphertext itself
	return json.Marshal(wrapAAD{Format: h.Format, Version: h.Version, Wrap: wCopy})
}

func (h *Header) DecodeSalt() ([]byte, error) {
	if h.KDF == nil {
		return nil, fmt.Errorf("DecodeSalt: not a v1 header")
	}
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
