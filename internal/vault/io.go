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

// Open reads, decodes, and decrypts a vault file. The returned plaintext
// JSON is meant to be parsed by the store package.
func Open(path string, passphrase []byte) (plaintext []byte, key []byte, hdr *Header, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, err
	}
	hdr = &Header{}
	if err := json.Unmarshal(raw, hdr); err != nil {
		return nil, nil, nil, fmt.Errorf("not a kpot vault file: %w", err)
	}
	if err := hdr.Validate(); err != nil {
		return nil, nil, nil, err
	}

	salt, err := hdr.DecodeSalt()
	if err != nil {
		return nil, nil, nil, err
	}
	nonce, err := hdr.DecodeNonce()
	if err != nil {
		return nil, nil, nil, err
	}
	ciphertext, err := hdr.DecodePayload()
	if err != nil {
		return nil, nil, nil, err
	}

	key = crypto.DeriveKey(passphrase, salt, hdr.KDF.Params)
	aad, err := hdr.AAD()
	if err != nil {
		crypto.Zero(key)
		return nil, nil, nil, err
	}
	plaintext, err = crypto.Open(key, nonce, ciphertext, aad)
	if err != nil {
		crypto.Zero(key)
		return nil, nil, nil, err
	}
	return plaintext, key, hdr, nil
}

// Create writes a brand new vault. Returns the derived key (caller is
// responsible for zeroing it when done) and the chosen header.
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
		Version: FormatVersion,
		KDF: KDFSection{
			Name:   "argon2id",
			Salt:   base64.StdEncoding.EncodeToString(salt),
			Params: params,
		},
		Cipher: CipherSection{
			Name: "xchacha20-poly1305",
		},
	}
	if err := writeWithKey(path, plaintext, key, hdr); err != nil {
		crypto.Zero(key)
		return nil, nil, err
	}
	return key, hdr, nil
}

// Rekey re-encrypts the vault under newPassphrase. A fresh salt and a
// new derived key replace the old ones; the file is written atomically
// (with the same .bak invariant as Save) and then the post-write .bak
// is removed because it would otherwise still be encrypted with the
// previous passphrase — defeating the point of the rotation.
func Rekey(path string, plaintext, newPassphrase []byte) error {
	salt, err := crypto.NewSalt()
	if err != nil {
		return err
	}
	params := crypto.DefaultArgon2idParams()
	key := crypto.DeriveKey(newPassphrase, salt, params)
	defer crypto.Zero(key)

	hdr := &Header{
		Format:  FormatName,
		Version: FormatVersion,
		KDF: KDFSection{
			Name:   "argon2id",
			Salt:   base64.StdEncoding.EncodeToString(salt),
			Params: params,
		},
		Cipher: CipherSection{
			Name: "xchacha20-poly1305",
		},
	}
	if err := writeWithKey(path, plaintext, key, hdr); err != nil {
		return err
	}

	// Wipe the just-created .bak — it still holds OLD-passphrase
	// ciphertext and would otherwise be a leak surface for the very
	// passphrase the user is rotating away from.
	if err := os.Remove(path + ".bak"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("rekey wrote %s but failed to remove stale .bak: %w", path, err)
	}
	return nil
}

// Save re-encrypts plaintext with the existing key and KDF section,
// generating a fresh nonce, and atomically replaces the vault file.
// The previous file is preserved as <path>.bak.
func Save(path string, plaintext, key []byte, hdr *Header) error {
	newHdr := *hdr
	newHdr.Cipher.Nonce = ""
	newHdr.Payload = ""
	return writeWithKey(path, plaintext, key, &newHdr)
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
