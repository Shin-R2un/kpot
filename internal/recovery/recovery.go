// Package recovery encodes and decodes the two recovery-key formats
// supported by kpot v0.3+: BIP-39 mnemonics (12 or 24 words) and a
// 32-byte secret key rendered as Crockford Base32. Recovery keys
// derive a Key Encryption Key (KEK) used to wrap the vault's Data
// Encryption Key (DEK); the entropy in either format is high enough
// that we use cheap KDFs (PBKDF2 / HKDF) rather than Argon2id.
package recovery

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/pbkdf2"
)

// Type names the recovery format stored in the vault header.
type Type string

const (
	TypeSeedBIP39  Type = "seed-bip39"
	TypeSecretKey  Type = "secret-key"
	pbkdf2IterSeed      = 2048 // BIP-39 spec value; sufficient since seed entropy is 128/256 bits
	kekLen              = 32
	secretKeyBytes      = 32
)

// ParseType returns the canonical Type for a user-supplied flag value.
func ParseType(s string) (Type, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "seed", "seed-bip39", "bip39":
		return TypeSeedBIP39, nil
	case "key", "secret-key", "secretkey":
		return TypeSecretKey, nil
	default:
		return "", fmt.Errorf("unknown recovery type %q (expected: seed | key)", s)
	}
}

// GenerateSeed produces a fresh BIP-39 mnemonic. words must be 12 or 24.
// The returned string is space-separated and ready for display.
func GenerateSeed(words int) (mnemonic string, err error) {
	bits, err := mnemonicEntropyBits(words)
	if err != nil {
		return "", err
	}
	entropy, err := bip39.NewEntropy(bits)
	if err != nil {
		return "", fmt.Errorf("entropy: %w", err)
	}
	return bip39.NewMnemonic(entropy)
}

// SeedToKEK validates the mnemonic and derives a 32-byte KEK using the
// BIP-39 standard PBKDF2-HMAC-SHA512 (iteration count 2048, salt
// "mnemonic" + optional empty passphrase). We then take the first 32
// bytes as the KEK. We deliberately don't accept a BIP-39 passphrase
// (the "25th word") in v0.3 — added complexity, marginal safety win.
func SeedToKEK(mnemonic string) ([]byte, error) {
	mnemonic = NormalizeMnemonic(mnemonic)
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, errors.New("invalid recovery seed (wrong checksum or unknown words)")
	}
	seed := bip39.NewSeed(mnemonic, "")
	kek := pbkdf2.Key(seed, []byte("kpot-recovery-seed-bip39"), pbkdf2IterSeed, kekLen, sha256.New)
	return kek, nil
}

// NormalizeMnemonic lower-cases and collapses whitespace so paste
// noise (extra spaces, CR/LF, weirdly capitalized words) doesn't
// reject a legitimate seed.
func NormalizeMnemonic(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// GenerateSecretKey returns 32 bytes of randomness rendered as the
// canonical Crockford Base32 form (52 chars, hyphenated for read-aloud).
func GenerateSecretKey() (display string, raw []byte, err error) {
	raw = make([]byte, secretKeyBytes)
	if _, err = rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("secret-key generation: %w", err)
	}
	return FormatSecretKey(raw), raw, nil
}

// SecretKeyToKEK derives a 32-byte KEK from the raw secret key using
// HKDF-SHA256. The salt is fixed (domain separation), info distinguishes
// this derivation from any future ones.
func SecretKeyToKEK(raw []byte) ([]byte, error) {
	if len(raw) != secretKeyBytes {
		return nil, fmt.Errorf("secret key must be %d bytes, got %d", secretKeyBytes, len(raw))
	}
	r := hkdf.New(sha256.New, raw, []byte("kpot-recovery-secret-key"), []byte("kek-v1"))
	kek := make([]byte, kekLen)
	if _, err := r.Read(kek); err != nil {
		return nil, fmt.Errorf("hkdf: %w", err)
	}
	return kek, nil
}

// ParseSecretKey accepts either the hyphenated display form or a raw
// Crockford Base32 stream and returns the 32-byte secret. Mistypes
// like O→0 / I→1 / L→1 / U→V are silently corrected (Crockford spec).
func ParseSecretKey(s string) ([]byte, error) {
	cleaned := normalizeCrockford(s)
	if len(cleaned) == 0 {
		return nil, errors.New("empty secret key")
	}
	raw, err := decodeCrockford(cleaned)
	if err != nil {
		return nil, err
	}
	if len(raw) != secretKeyBytes {
		return nil, fmt.Errorf("secret key must decode to %d bytes, got %d", secretKeyBytes, len(raw))
	}
	return raw, nil
}

// FormatSecretKey renders raw bytes as Crockford Base32 with hyphen
// separators every 8 chars (e.g. AAAAAAAA-BBBBBBBB-...) so eyes can
// track position when copying onto paper.
func FormatSecretKey(raw []byte) string {
	enc := encodeCrockford(raw)
	var b strings.Builder
	for i, r := range enc {
		if i > 0 && i%8 == 0 {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// HexFingerprint returns a short tag for diagnostics that doesn't
// reveal the secret. Currently the first 4 bytes of SHA-256 hex.
func HexFingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:4])
}

func mnemonicEntropyBits(words int) (int, error) {
	switch words {
	case 12:
		return 128, nil
	case 24:
		return 256, nil
	default:
		return 0, fmt.Errorf("words must be 12 or 24 (got %d)", words)
	}
}

// --- Crockford Base32 (RFC 4648 alternative) ---
// Excludes I, L, O, U to dodge visually-confused digits/letters.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var crockfordRev = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	for i, r := range crockfordAlphabet {
		t[r] = int8(i)
		t[lowerByte(byte(r))] = int8(i)
	}
	// Crockford forgiveness: O→0, I→1, L→1
	t['O'], t['o'] = 0, 0
	t['I'], t['i'] = 1, 1
	t['L'], t['l'] = 1, 1
	return t
}()

func lowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func encodeCrockford(b []byte) string {
	// 5 bits per output char. Pack big-endian, padded with zeros.
	if len(b) == 0 {
		return ""
	}
	bits := len(b) * 8
	outLen := (bits + 4) / 5
	out := make([]byte, outLen)
	var buf uint64
	var bufBits uint
	idx := 0
	for _, v := range b {
		buf = (buf << 8) | uint64(v)
		bufBits += 8
		for bufBits >= 5 {
			bufBits -= 5
			out[idx] = crockfordAlphabet[(buf>>bufBits)&0x1F]
			idx++
		}
	}
	if bufBits > 0 {
		out[idx] = crockfordAlphabet[(buf<<(5-bufBits))&0x1F]
		idx++
	}
	return string(out[:idx])
}

func decodeCrockford(s string) ([]byte, error) {
	bits := len(s) * 5
	outLen := bits / 8
	out := make([]byte, 0, outLen)
	var buf uint64
	var bufBits uint
	for i := 0; i < len(s); i++ {
		v := crockfordRev[s[i]]
		if v < 0 {
			return nil, fmt.Errorf("invalid character %q at position %d", s[i], i)
		}
		buf = (buf << 5) | uint64(v)
		bufBits += 5
		if bufBits >= 8 {
			bufBits -= 8
			out = append(out, byte((buf>>bufBits)&0xFF))
		}
	}
	return out, nil
}

func normalizeCrockford(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case ' ', '\t', '\n', '\r', '-', '_':
			continue
		}
		b.WriteByte(c)
	}
	return strings.ToUpper(b.String())
}
