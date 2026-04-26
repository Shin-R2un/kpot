// Package bundle defines the .kpb (kpot bundle) file format used to
// transfer a hand-picked subset of notes from one vault to another
// without merging entire vault files.
//
// A bundle is a self-contained encrypted blob: it carries everything
// needed to decrypt itself given the source passphrase, so the
// receiving side never needs the source vault file at all. The
// recipient runs `kpot <their-vault> import-bundle <file.kpb>`,
// types the source passphrase to unlock the bundle, then chooses
// whether to merge the contained notes into their own vault.
//
// Crypto is the same primitives the vault package uses:
// Argon2id over the passphrase + XChaCha20-Poly1305 AEAD, with
// header bound as AAD so KDF/cipher swaps are detected on open.
package bundle

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/r2un/kpot/internal/crypto"
	"github.com/r2un/kpot/internal/store"
)

const (
	FormatName    = "kpot-bundle"
	FormatVersion = 1

	cipherName = "xchacha20-poly1305"
	kdfName    = "argon2id"
)

// Note mirrors store.Note for inclusion in the bundle payload. We
// duplicate the struct shape (instead of importing store.Note) so the
// bundle format is independent of the in-memory vault representation
// — store can grow new fields without breaking the bundle envelope.
type Note struct {
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Payload is the decrypted bundle contents — a map of note name to
// note body + timestamps, plus a creation timestamp for the bundle
// itself.
type Payload struct {
	Version   int              `json:"version"`
	CreatedAt time.Time        `json:"created_at"`
	Notes     map[string]*Note `json:"notes"`
}

type kdfSection struct {
	Name   string                `json:"name"`
	Salt   string                `json:"salt"`
	Params crypto.Argon2idParams `json:"params"`
}

type cipherSection struct {
	Name string `json:"name"`
}

// Header is the unencrypted envelope. The wrap_nonce / wrapped_bek
// pair holds the random Bundle Encryption Key (BEK), encrypted by a
// passphrase-derived KEK. The payload is encrypted with the BEK.
type Header struct {
	Format     string        `json:"format"`
	Version    int           `json:"version"`
	KDF        kdfSection    `json:"kdf"`
	WrapNonce  string        `json:"wrap_nonce"`
	WrappedBEK string        `json:"wrapped_bek"`
	Cipher     cipherSection `json:"cipher"`
	Nonce      string        `json:"nonce"`   // payload nonce
	Payload    string        `json:"payload"` // BEK-encrypted JSON
}

func (h *Header) validate() error {
	if h.Format != FormatName {
		return fmt.Errorf("not a kpot bundle (format=%q)", h.Format)
	}
	if h.Version != FormatVersion {
		return fmt.Errorf("unsupported bundle version v%d (this build supports v%d)", h.Version, FormatVersion)
	}
	if h.KDF.Name != kdfName {
		return fmt.Errorf("unsupported KDF: %s", h.KDF.Name)
	}
	if h.Cipher.Name != cipherName {
		return fmt.Errorf("unsupported cipher: %s", h.Cipher.Name)
	}
	if err := h.KDF.Params.Validate(); err != nil {
		return err
	}
	if h.WrapNonce == "" || h.WrappedBEK == "" || h.Nonce == "" || h.Payload == "" {
		return errors.New("bundle header has missing fields")
	}
	return nil
}

// aad returns the canonical AAD bytes — header without the
// passphrase-dependent secrets (WrappedBEK, Payload). Binding KDF
// params + cipher choice means a downgrade attack on either is
// detected at unwrap or payload decrypt time.
func (h *Header) aad() ([]byte, error) {
	type aadDoc struct {
		Format    string        `json:"format"`
		Version   int           `json:"version"`
		KDF       kdfSection    `json:"kdf"`
		WrapNonce string        `json:"wrap_nonce"`
		Cipher    cipherSection `json:"cipher"`
		Nonce     string        `json:"nonce"`
	}
	return json.Marshal(aadDoc{
		Format:    h.Format,
		Version:   h.Version,
		KDF:       h.KDF,
		WrapNonce: h.WrapNonce,
		Cipher:    h.Cipher,
		Nonce:     h.Nonce,
	})
}

// FromStoreNotes converts a sub-set of store.Note values into the
// bundle's own Note shape. Returns an error if names is empty or any
// note is missing from src.
func FromStoreNotes(src map[string]*store.Note, names []string) (map[string]*Note, error) {
	if len(names) == 0 {
		return nil, errors.New("no notes selected")
	}
	out := make(map[string]*Note, len(names))
	for _, name := range names {
		n, ok := src[name]
		if !ok {
			return nil, fmt.Errorf("note %q not found", name)
		}
		out[name] = &Note{Body: n.Body, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt}
	}
	return out, nil
}

// SortedNames returns the keys of notes in deterministic order. Used
// by import previews to show entries consistently.
func SortedNames(notes map[string]*Note) []string {
	out := make([]string, 0, len(notes))
	for k := range notes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Build encrypts notes into a self-contained bundle. The bundle is
// secured by a fresh random BEK (Bundle Encryption Key); the BEK is
// then wrapped with a KEK derived from passphrase via Argon2id. The
// returned bytes can be written verbatim to a .kpb file.
func Build(notes map[string]*Note, passphrase []byte) ([]byte, error) {
	if len(notes) == 0 {
		return nil, errors.New("bundle requires at least one note")
	}
	if len(passphrase) == 0 {
		return nil, errors.New("bundle requires a passphrase")
	}

	// 1. Derive KEK from passphrase + fresh salt.
	salt, err := crypto.NewSalt()
	if err != nil {
		return nil, err
	}
	params := crypto.DefaultArgon2idParams()
	kek := crypto.DeriveKey(passphrase, salt, params)
	defer crypto.Zero(kek)

	// 2. Generate fresh BEK (32B) and wrap it with KEK.
	bek, err := crypto.NewDEK()
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(bek)

	wrapNonce, err := crypto.NewNonce()
	if err != nil {
		return nil, err
	}
	payloadNonce, err := crypto.NewNonce()
	if err != nil {
		return nil, err
	}

	// Build header up-front so we can compute AAD without secrets.
	hdr := &Header{
		Format:    FormatName,
		Version:   FormatVersion,
		KDF:       kdfSection{Name: kdfName, Salt: base64.StdEncoding.EncodeToString(salt), Params: params},
		WrapNonce: base64.StdEncoding.EncodeToString(wrapNonce),
		Cipher:    cipherSection{Name: cipherName},
		Nonce:     base64.StdEncoding.EncodeToString(payloadNonce),
	}
	aad, err := hdr.aad()
	if err != nil {
		return nil, err
	}

	wrappedBEK, err := crypto.Wrap(kek, wrapNonce, bek, aad)
	if err != nil {
		return nil, err
	}
	hdr.WrappedBEK = base64.StdEncoding.EncodeToString(wrappedBEK)

	// 3. Marshal payload and encrypt with BEK.
	payload := Payload{Version: 1, CreatedAt: time.Now().UTC(), Notes: notes}
	pt, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(pt)

	ct, err := crypto.Seal(bek, payloadNonce, pt, aad)
	if err != nil {
		return nil, err
	}
	hdr.Payload = base64.StdEncoding.EncodeToString(ct)

	return json.MarshalIndent(hdr, "", "  ")
}

// Open is the inverse of Build. Returns ErrAuthFailed (re-exported
// from crypto) when the passphrase is wrong or the bundle is tampered;
// callers should map both to a single user-visible message to avoid
// leaking which it was.
func Open(blob []byte, passphrase []byte) (map[string]*Note, error) {
	hdr := &Header{}
	if err := json.Unmarshal(blob, hdr); err != nil {
		return nil, fmt.Errorf("not a kpot bundle file: %w", err)
	}
	if err := hdr.validate(); err != nil {
		return nil, err
	}

	salt, err := base64.StdEncoding.DecodeString(hdr.KDF.Salt)
	if err != nil {
		return nil, fmt.Errorf("invalid salt encoding: %w", err)
	}
	wrapNonce, err := base64.StdEncoding.DecodeString(hdr.WrapNonce)
	if err != nil {
		return nil, fmt.Errorf("invalid wrap_nonce encoding: %w", err)
	}
	wrapped, err := base64.StdEncoding.DecodeString(hdr.WrappedBEK)
	if err != nil {
		return nil, fmt.Errorf("invalid wrapped_bek encoding: %w", err)
	}
	payloadNonce, err := base64.StdEncoding.DecodeString(hdr.Nonce)
	if err != nil {
		return nil, fmt.Errorf("invalid nonce encoding: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(hdr.Payload)
	if err != nil {
		return nil, fmt.Errorf("invalid payload encoding: %w", err)
	}

	aad, err := hdr.aad()
	if err != nil {
		return nil, err
	}

	kek := crypto.DeriveKey(passphrase, salt, hdr.KDF.Params)
	defer crypto.Zero(kek)

	bek, err := crypto.Unwrap(kek, wrapNonce, wrapped, aad)
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(bek)

	pt, err := crypto.Open(bek, payloadNonce, ct, aad)
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(pt)

	var payload Payload
	if err := json.Unmarshal(pt, &payload); err != nil {
		return nil, fmt.Errorf("decode bundle payload: %w", err)
	}
	if payload.Version != 1 {
		return nil, fmt.Errorf("unsupported bundle payload version: %d", payload.Version)
	}
	if len(payload.Notes) == 0 {
		return nil, errors.New("bundle payload contains no notes")
	}
	return payload.Notes, nil
}
