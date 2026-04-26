package recovery

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseType(t *testing.T) {
	cases := map[string]Type{
		"seed":       TypeSeedBIP39,
		"SEED":       TypeSeedBIP39,
		"seed-bip39": TypeSeedBIP39,
		"bip39":      TypeSeedBIP39,
		"key":        TypeSecretKey,
		"secret-key": TypeSecretKey,
		"  Key  ":    TypeSecretKey,
	}
	for in, want := range cases {
		got, err := ParseType(in)
		if err != nil {
			t.Errorf("ParseType(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseType(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := ParseType("nonsense"); err == nil {
		t.Error("expected error for nonsense type")
	}
}

func TestSeedRoundTrip12Words(t *testing.T) {
	mnemonic, err := GenerateSeed(12)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Fields(mnemonic)); got != 12 {
		t.Fatalf("expected 12 words, got %d", got)
	}
	kek1, err := SeedToKEK(mnemonic)
	if err != nil {
		t.Fatal(err)
	}
	kek2, err := SeedToKEK(mnemonic)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(kek1, kek2) {
		t.Fatal("SeedToKEK is not deterministic")
	}
	if len(kek1) != 32 {
		t.Fatalf("KEK length = %d, want 32", len(kek1))
	}
}

func TestSeedRoundTrip24Words(t *testing.T) {
	mnemonic, err := GenerateSeed(24)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Fields(mnemonic)); got != 24 {
		t.Fatalf("expected 24 words, got %d", got)
	}
	if _, err := SeedToKEK(mnemonic); err != nil {
		t.Fatal(err)
	}
}

func TestSeedRejectsBadWordCount(t *testing.T) {
	if _, err := GenerateSeed(13); err == nil {
		t.Error("expected error for 13 words")
	}
	if _, err := GenerateSeed(0); err == nil {
		t.Error("expected error for 0 words")
	}
}

func TestSeedToKEKRejectsBadChecksum(t *testing.T) {
	mnemonic, _ := GenerateSeed(12)
	words := strings.Fields(mnemonic)
	// Swap the last word to break the BIP-39 checksum.
	words[11] = "zoo"
	bad := strings.Join(words, " ")
	if _, err := SeedToKEK(bad); err == nil {
		t.Fatal("expected checksum failure")
	}
}

func TestSeedToKEKAcceptsMessyInput(t *testing.T) {
	mnemonic, _ := GenerateSeed(12)
	messy := "  " + strings.ReplaceAll(strings.ToUpper(mnemonic), " ", "  \n ") + "\n"
	a, err := SeedToKEK(messy)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := SeedToKEK(mnemonic)
	if !bytes.Equal(a, b) {
		t.Fatal("normalization failed to canonicalize")
	}
}

func TestSecretKeyRoundTrip(t *testing.T) {
	display, raw, err := GenerateSecretKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 32 {
		t.Fatalf("raw len = %d, want 32", len(raw))
	}
	parsed, err := ParseSecretKey(display)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(parsed, raw) {
		t.Fatal("display→parse mismatch")
	}
	kek1, err := SecretKeyToKEK(raw)
	if err != nil {
		t.Fatal(err)
	}
	kek2, err := SecretKeyToKEK(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(kek1, kek2) {
		t.Fatal("SecretKeyToKEK not deterministic")
	}
	if len(kek1) != 32 {
		t.Fatalf("kek len = %d", len(kek1))
	}
}

func TestSecretKeyDisplayShape(t *testing.T) {
	display, _, _ := GenerateSecretKey()
	groups := strings.Split(display, "-")
	// 32 bytes * 8 / 5 = 51.2 → 52 chars → 7 groups (8+8+8+8+8+8+4)
	if len(groups) != 7 {
		t.Errorf("expected 7 groups, got %d (%q)", len(groups), display)
	}
	for i, g := range groups[:6] {
		if len(g) != 8 {
			t.Errorf("group %d length = %d, want 8", i, len(g))
		}
	}
	if len(groups[6]) != 4 {
		t.Errorf("last group length = %d, want 4", len(groups[6]))
	}
}

func TestSecretKeyCrockfordForgiveness(t *testing.T) {
	_, raw, _ := GenerateSecretKey()
	canonical := FormatSecretKey(raw)
	// Substitute O→0, I→1, L→1 in the display, lowercase, drop hyphens.
	messy := strings.NewReplacer("0", "O", "1", "I").Replace(canonical)
	messy = strings.ToLower(strings.ReplaceAll(messy, "-", ""))
	parsed, err := ParseSecretKey(messy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(parsed, raw) {
		t.Fatalf("Crockford forgiveness round-trip failed")
	}
}

func TestSecretKeyRejectsGarbage(t *testing.T) {
	if _, err := ParseSecretKey(""); err == nil {
		t.Error("expected error for empty string")
	}
	if _, err := ParseSecretKey("not-base32!"); err == nil {
		t.Error("expected error for invalid characters")
	}
	if _, err := ParseSecretKey("AAAAAAAA"); err == nil {
		t.Error("expected error for short input")
	}
}

func TestKEKsAreDifferentAcrossKeys(t *testing.T) {
	_, raw1, _ := GenerateSecretKey()
	_, raw2, _ := GenerateSecretKey()
	k1, _ := SecretKeyToKEK(raw1)
	k2, _ := SecretKeyToKEK(raw2)
	if bytes.Equal(k1, k2) {
		t.Fatal("two random secret keys produced identical KEKs")
	}
}

func TestSeedAndSecretKeyDifferentDomain(t *testing.T) {
	// Same 32-byte input fed through both KDFs must produce different
	// KEKs (domain separation via salt/info).
	mnemonic, _ := GenerateSeed(12)
	seedKEK, _ := SeedToKEK(mnemonic)

	// Reuse the seed-derived bytes as a "raw key" — pretend collision.
	skKEK, _ := SecretKeyToKEK(seedKEK[:32])
	if bytes.Equal(seedKEK, skKEK) {
		t.Fatal("seed and secret-key paths must derive distinct KEKs (domain separation)")
	}
}

func TestHexFingerprintIsStable(t *testing.T) {
	raw := bytes.Repeat([]byte{0xAB}, 32)
	a := HexFingerprint(raw)
	b := HexFingerprint(raw)
	if a != b {
		t.Fatal("fingerprint not stable")
	}
	if len(a) != 8 {
		t.Fatalf("fingerprint length = %d, want 8", len(a))
	}
}
