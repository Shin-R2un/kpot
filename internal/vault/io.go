package vault

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/r2un/kpot/internal/crypto"
)

// ErrNoRecovery is returned when a recovery-key open is attempted on a
// vault that doesn't have a recovery wrap configured.
var ErrNoRecovery = errors.New("vault has no recovery wrap configured")

// Open reads, decodes, and decrypts a vault file using a passphrase.
// Handles both v1 (direct argon2id key over payload) and v2 (argon2id
// over passphrase → KEK → unwrap DEK → decrypt payload). The returned
// `key` is whatever encrypts the payload; the caller treats it as
// opaque and passes it back to Save.
func Open(path string, passphrase []byte) (plaintext []byte, key []byte, hdr *Header, err error) {
	hdr, err = readHeader(path)
	if err != nil {
		return nil, nil, nil, err
	}
	switch hdr.Version {
	case 1:
		return openV1(hdr, passphrase)
	case 2:
		return openV2WithPassphrase(hdr, passphrase)
	}
	return nil, nil, nil, fmt.Errorf("unhandled version %d", hdr.Version)
}

// OpenWithKey skips key derivation entirely and decrypts the payload
// directly with the supplied 32-byte key. The key meaning depends on
// the vault version: for v1 it's the Argon2id-derived passphrase key,
// for v2 it's the DEK. The caller (typically the OS keychain cache
// path) treats the key as opaque — wrong key surfaces as
// crypto.ErrAuthFailed, same as a passphrase mismatch.
func OpenWithKey(path string, key []byte) (plaintext []byte, hdr *Header, err error) {
	hdr, err = readHeader(path)
	if err != nil {
		return nil, nil, err
	}
	plaintext, err = decryptPayload(hdr, key)
	if err != nil {
		return nil, nil, err
	}
	return plaintext, hdr, nil
}

// OpenWithRecovery opens a v2 vault using a pre-derived recovery KEK
// (caller derived it from a seed mnemonic or a secret-key — see the
// recovery package). v1 vaults have no recovery wrap; calling this on
// one returns ErrNoRecovery.
func OpenWithRecovery(path string, recoveryKEK []byte) (plaintext []byte, dek []byte, hdr *Header, err error) {
	hdr, err = readHeader(path)
	if err != nil {
		return nil, nil, nil, err
	}
	if hdr.Version != 2 || hdr.RecoveryWrap == nil {
		return nil, nil, nil, ErrNoRecovery
	}
	dek, err = unwrapDEK(hdr, hdr.RecoveryWrap, recoveryKEK)
	if err != nil {
		return nil, nil, nil, err
	}
	plaintext, err = decryptPayload(hdr, dek)
	if err != nil {
		crypto.Zero(dek)
		return nil, nil, nil, err
	}
	return plaintext, dek, hdr, nil
}

func openV1(hdr *Header, passphrase []byte) ([]byte, []byte, *Header, error) {
	salt, err := hdr.DecodeSalt()
	if err != nil {
		return nil, nil, nil, err
	}
	nonce, err := hdr.DecodeNonce()
	if err != nil {
		return nil, nil, nil, err
	}
	ct, err := hdr.DecodePayload()
	if err != nil {
		return nil, nil, nil, err
	}
	key := crypto.DeriveKey(passphrase, salt, hdr.KDF.Params)
	aad, err := hdr.AAD()
	if err != nil {
		crypto.Zero(key)
		return nil, nil, nil, err
	}
	pt, err := crypto.Open(key, nonce, ct, aad)
	if err != nil {
		crypto.Zero(key)
		return nil, nil, nil, err
	}
	return pt, key, hdr, nil
}

func openV2WithPassphrase(hdr *Header, passphrase []byte) ([]byte, []byte, *Header, error) {
	wrap := hdr.PassphraseWrap
	salt, err := base64.StdEncoding.DecodeString(wrap.KDF.Salt)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("passphrase wrap salt: %w", err)
	}
	kek := crypto.DeriveKey(passphrase, salt, *wrap.KDF.Params)
	defer crypto.Zero(kek)

	dek, err := unwrapDEK(hdr, wrap, kek)
	if err != nil {
		return nil, nil, nil, err
	}
	pt, err := decryptPayload(hdr, dek)
	if err != nil {
		crypto.Zero(dek)
		return nil, nil, nil, err
	}
	return pt, dek, hdr, nil
}

func unwrapDEK(hdr *Header, w *Wrap, kek []byte) ([]byte, error) {
	nonce, err := base64.StdEncoding.DecodeString(w.Nonce)
	if err != nil {
		return nil, fmt.Errorf("wrap nonce: %w", err)
	}
	wrapped, err := base64.StdEncoding.DecodeString(w.WrappedDEK)
	if err != nil {
		return nil, fmt.Errorf("wrapped_dek: %w", err)
	}
	aad, err := hdr.WrapAAD(w)
	if err != nil {
		return nil, err
	}
	return crypto.Unwrap(kek, nonce, wrapped, aad)
}

func decryptPayload(hdr *Header, key []byte) ([]byte, error) {
	nonce, err := hdr.DecodeNonce()
	if err != nil {
		return nil, err
	}
	ct, err := hdr.DecodePayload()
	if err != nil {
		return nil, err
	}
	aad, err := hdr.AAD()
	if err != nil {
		return nil, err
	}
	return crypto.Open(key, nonce, ct, aad)
}

// PeekHeader reads, parses, and validates the vault header WITHOUT
// attempting any decryption. Useful for callers that need to know the
// vault's version or whether it has a recovery wrap before deciding
// what to prompt the user for.
func PeekHeader(path string) (*Header, error) {
	return readHeader(path)
}

func readHeader(path string) (*Header, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	hdr := &Header{}
	if err := json.Unmarshal(raw, hdr); err != nil {
		return nil, fmt.Errorf("not a kpot vault file: %w", err)
	}
	if err := hdr.Validate(); err != nil {
		return nil, err
	}
	return hdr, nil
}

// Create writes a brand-new v1 vault. Preserved so existing tests and
// callers that explicitly want the old envelope (no recovery option)
// keep working. New CLI flows should call CreateV2WithRecovery.
func Create(path string, passphrase, plaintext []byte) (key []byte, hdr *Header, err error) {
	if _, statErr := os.Stat(path); statErr == nil {
		return nil, nil, fmt.Errorf("%s already exists. Refusing to overwrite", path)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, nil, statErr
	}

	salt, err := crypto.NewSalt()
	if err != nil {
		return nil, nil, err
	}
	params := crypto.DefaultArgon2idParams()
	key = crypto.DeriveKey(passphrase, salt, params)

	hdr = &Header{
		Format:  FormatName,
		Version: 1,
		KDF: &KDFSection{
			Name:   KDFArgon2id,
			Salt:   base64.StdEncoding.EncodeToString(salt),
			Params: params,
		},
		Cipher: CipherSection{Name: "xchacha20-poly1305"},
	}
	if err := writeWithKey(path, plaintext, key, hdr); err != nil {
		crypto.Zero(key)
		return nil, nil, err
	}
	return key, hdr, nil
}

// CreateV2WithRecovery is the v0.3+ init path: generates a random DEK,
// builds a passphrase wrap (mandatory) and a recovery wrap (always
// present in v2), encrypts the payload with the DEK, writes
// atomically. Returns the DEK so subsequent saves don't re-derive
// anything. recoveryWrapKind ∈ {WrapKindSeed, WrapKindSecretKey}; the
// caller already derived recoveryKEK in the recovery package from a
// freshly generated seed/key.
func CreateV2WithRecovery(
	path string,
	passphrase []byte,
	recoveryWrapKind string,
	recoveryKEK []byte,
	plaintext []byte,
) (dek []byte, hdr *Header, err error) {
	if _, statErr := os.Stat(path); statErr == nil {
		return nil, nil, fmt.Errorf("%s already exists. Refusing to overwrite", path)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, nil, statErr
	}

	dek, err = crypto.NewDEK()
	if err != nil {
		return nil, nil, err
	}

	pw, pkek, err := buildPassphraseWrap(passphrase)
	if err != nil {
		crypto.Zero(dek)
		return nil, nil, err
	}
	defer crypto.Zero(pkek)

	rw, err := buildRecoveryWrap(recoveryWrapKind)
	if err != nil {
		crypto.Zero(dek)
		return nil, nil, err
	}

	hdr = &Header{
		Format:         FormatName,
		Version:        2,
		PassphraseWrap: pw,
		RecoveryWrap:   rw,
		Cipher:         CipherSection{Name: "xchacha20-poly1305"},
	}
	if err := wrapDEKInto(hdr, pw, pkek, dek); err != nil {
		crypto.Zero(dek)
		return nil, nil, err
	}
	if err := wrapDEKInto(hdr, rw, recoveryKEK, dek); err != nil {
		crypto.Zero(dek)
		return nil, nil, err
	}
	if err := writeWithKey(path, plaintext, dek, hdr); err != nil {
		crypto.Zero(dek)
		return nil, nil, err
	}
	return dek, hdr, nil
}

func buildPassphraseWrap(passphrase []byte) (*Wrap, []byte, error) {
	salt, err := crypto.NewSalt()
	if err != nil {
		return nil, nil, err
	}
	params := crypto.DefaultArgon2idParams()
	kek := crypto.DeriveKey(passphrase, salt, params)
	w := &Wrap{
		Kind: WrapKindPassphrase,
		KDF: WrapKDF{
			Name:   KDFArgon2id,
			Salt:   base64.StdEncoding.EncodeToString(salt),
			Params: &params,
		},
	}
	return w, kek, nil
}

func buildRecoveryWrap(kind string) (*Wrap, error) {
	switch kind {
	case WrapKindSeed:
		return &Wrap{
			Kind: WrapKindSeed,
			KDF: WrapKDF{
				Name:       KDFPBKDF2SHA512,
				Iterations: 2048,
			},
		}, nil
	case WrapKindSecretKey:
		return &Wrap{
			Kind: WrapKindSecretKey,
			KDF:  WrapKDF{Name: KDFHKDFSHA256},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported recovery kind %q", kind)
	}
}

// wrapDEKInto encrypts dek using kek and stores the result inside the
// wrap (Nonce + WrappedDEK). The wrap's KDF section is bound as AAD,
// so any later mutation breaks unwrap.
func wrapDEKInto(hdr *Header, w *Wrap, kek, dek []byte) error {
	nonce, err := crypto.NewNonce()
	if err != nil {
		return err
	}
	w.Nonce = base64.StdEncoding.EncodeToString(nonce)
	aad, err := hdr.WrapAAD(w)
	if err != nil {
		return err
	}
	wrapped, err := crypto.Wrap(kek, nonce, dek, aad)
	if err != nil {
		return err
	}
	w.WrappedDEK = base64.StdEncoding.EncodeToString(wrapped)
	return nil
}

// Save re-encrypts plaintext with the existing key (v1: argon2id key;
// v2: DEK) and the existing header, generating a fresh payload nonce,
// and atomically replaces the vault file. Wraps are NOT touched.
func Save(path string, plaintext, key []byte, hdr *Header) error {
	newHdr := *hdr
	newHdr.Cipher.Nonce = ""
	newHdr.Payload = ""
	return writeWithKey(path, plaintext, key, &newHdr)
}

// Rekey re-encrypts a v1 vault under newPassphrase. v2 vaults must use
// RekeyV2 instead (the DEK is preserved so the recovery wrap stays
// valid). The post-write .bak is removed because it would otherwise
// hold OLD-passphrase ciphertext.
func Rekey(path string, plaintext, newPassphrase []byte) error {
	hdr, err := readHeader(path)
	if err != nil {
		return err
	}
	if hdr.Version != 1 {
		return fmt.Errorf("Rekey is v1-only; use RekeyV2 for v%d vaults", hdr.Version)
	}

	salt, err := crypto.NewSalt()
	if err != nil {
		return err
	}
	params := crypto.DefaultArgon2idParams()
	key := crypto.DeriveKey(newPassphrase, salt, params)
	defer crypto.Zero(key)

	newHdr := &Header{
		Format:  FormatName,
		Version: 1,
		KDF: &KDFSection{
			Name:   KDFArgon2id,
			Salt:   base64.StdEncoding.EncodeToString(salt),
			Params: params,
		},
		Cipher: CipherSection{Name: "xchacha20-poly1305"},
	}
	if err := writeWithKey(path, plaintext, key, newHdr); err != nil {
		return err
	}
	if err := os.Remove(path + ".bak"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("rekey wrote %s but failed to remove stale .bak: %w", path, err)
	}
	return nil
}

// RekeyV2 rotates the passphrase wrap on a v2 vault. The DEK and the
// recovery wrap are preserved (so seed-based recovery continues to
// work); only the passphrase wrap is rebuilt with a fresh salt and
// re-wrapped DEK. The payload is then re-sealed because the AAD now
// embeds the new passphrase wrap. .bak is removed for the same
// passphrase-leak reason as v1 rekey.
func RekeyV2(path string, dek, newPassphrase []byte) error {
	hdr, err := readHeader(path)
	if err != nil {
		return err
	}
	if hdr.Version != 2 {
		return fmt.Errorf("RekeyV2 requires v2 vault, got v%d", hdr.Version)
	}

	plaintext, err := decryptPayload(hdr, dek)
	if err != nil {
		return fmt.Errorf("RekeyV2: existing payload decrypt failed: %w", err)
	}
	defer crypto.Zero(plaintext)

	pw, pkek, err := buildPassphraseWrap(newPassphrase)
	if err != nil {
		return err
	}
	defer crypto.Zero(pkek)
	hdr.PassphraseWrap = pw

	if err := wrapDEKInto(hdr, pw, pkek, dek); err != nil {
		return err
	}
	// Recovery wrap is preserved unchanged: WrapAAD only binds a wrap
	// to format/version/its-own-fields (not to any sibling wrap), so
	// the recovery wrap's ciphertext stays valid across passphrase
	// rotation — the recovery KEK is never needed here. The payload
	// AAD does embed both wraps, so writeWithKey re-seals it below.
	if err := writeWithKey(path, plaintext, dek, hdr); err != nil {
		return err
	}
	if err := os.Remove(path + ".bak"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("RekeyV2 wrote %s but failed to remove stale .bak: %w", path, err)
	}
	return nil
}

func writeWithKey(path string, plaintext, key []byte, hdr *Header) error {
	nonce, err := crypto.NewNonce()
	if err != nil {
		return err
	}
	hdr.Cipher.Nonce = base64.StdEncoding.EncodeToString(nonce)

	aad, err := hdr.AAD()
	if err != nil {
		return err
	}
	ciphertext, err := crypto.Seal(key, nonce, plaintext, aad)
	if err != nil {
		return err
	}
	hdr.Payload = base64.StdEncoding.EncodeToString(ciphertext)

	encoded, err := json.MarshalIndent(hdr, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, encoded)
}

// atomicWrite writes data to path via a temp+rename, preserving the
// previous file as <path>.bak (one generation).
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp := path + ".tmp"

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	if _, err := os.Stat(path); err == nil {
		bak := path + ".bak"
		os.Remove(bak)
		if err := os.Rename(path, bak); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
