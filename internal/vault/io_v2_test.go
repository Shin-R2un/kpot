package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/r2un/kpot/internal/crypto"
)

// fakeRecoveryKEK gives tests a deterministic 32-byte KEK without
// pulling in the recovery package (vault must stay independent of it).
func fakeRecoveryKEK(seed byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = seed ^ byte(i)
	}
	return k
}

func TestCreateV2WithRecoverySeed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v2.kpot")
	pass := []byte("pass-1")
	rkek := fakeRecoveryKEK(0xA5)
	pt := []byte(`{"v":2,"notes":{"k":"v"}}`)

	dek, hdr, err := CreateV2WithRecovery(path, pass, WrapKindSeed, rkek, pt)
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(dek)
	if hdr.Version != 2 {
		t.Fatalf("hdr.Version = %d", hdr.Version)
	}
	if hdr.PassphraseWrap == nil || hdr.RecoveryWrap == nil {
		t.Fatalf("expected both wraps, got %+v / %+v", hdr.PassphraseWrap, hdr.RecoveryWrap)
	}
	if hdr.RecoveryWrap.Kind != WrapKindSeed {
		t.Errorf("recovery kind = %s", hdr.RecoveryWrap.Kind)
	}

	// Open with passphrase: should return the same plaintext.
	got, _, _, err := Open(path, pass)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pt) {
		t.Fatalf("passphrase open plaintext mismatch")
	}

	// Open with recovery KEK: same plaintext.
	got2, dek2, _, err := OpenWithRecovery(path, rkek)
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(dek2)
	if !bytes.Equal(got2, pt) {
		t.Fatalf("recovery open plaintext mismatch")
	}
	if !bytes.Equal(dek, dek2) {
		t.Fatalf("DEK from passphrase open and recovery open should match")
	}
}

func TestCreateV2WithRecoverySecretKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v2.kpot")
	pass := []byte("p")
	rkek := fakeRecoveryKEK(0x5A)
	pt := []byte(`{"v":2,"notes":{}}`)

	if _, _, err := CreateV2WithRecovery(path, pass, WrapKindSecretKey, rkek, pt); err != nil {
		t.Fatal(err)
	}
	hdr, err := readHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.RecoveryWrap.Kind != WrapKindSecretKey {
		t.Fatalf("recovery kind = %s", hdr.RecoveryWrap.Kind)
	}
	if hdr.RecoveryWrap.KDF.Name != KDFHKDFSHA256 {
		t.Fatalf("recovery KDF = %s", hdr.RecoveryWrap.KDF.Name)
	}
}

func TestV2WrongPassphraseAndWrongRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v2.kpot")
	rkek := fakeRecoveryKEK(0x33)
	if _, _, err := CreateV2WithRecovery(path, []byte("right"), WrapKindSeed, rkek, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := Open(path, []byte("wrong")); !errors.Is(err, crypto.ErrAuthFailed) {
		t.Fatalf("wrong passphrase should authfail, got %v", err)
	}
	if _, _, _, err := OpenWithRecovery(path, fakeRecoveryKEK(0x99)); !errors.Is(err, crypto.ErrAuthFailed) {
		t.Fatalf("wrong recovery KEK should authfail, got %v", err)
	}
}

func TestV2HeaderTamperDetected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v2.kpot")
	pass := []byte("p")
	rkek := fakeRecoveryKEK(0xCC)
	if _, _, err := CreateV2WithRecovery(path, pass, WrapKindSeed, rkek, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var h Header
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatal(err)
	}
	// Downgrade the iteration count on the passphrase wrap.
	h.PassphraseWrap.KDF.Params.Iterations = 1
	tampered, _ := json.MarshalIndent(&h, "", "  ")
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Open(path, pass); !errors.Is(err, crypto.ErrAuthFailed) {
		t.Fatalf("KDF tamper should authfail, got %v", err)
	}
}

func TestRekeyV2PreservesRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v2.kpot")
	rkek := fakeRecoveryKEK(0x77)
	pt := []byte(`{"v":2,"notes":{"k":"sk-original"}}`)

	dek, _, err := CreateV2WithRecovery(path, []byte("oldpass"), WrapKindSeed, rkek, pt)
	if err != nil {
		t.Fatal(err)
	}

	if err := RekeyV2(path, dek, []byte("newpass")); err != nil {
		t.Fatal(err)
	}

	// Old passphrase fails.
	if _, _, _, err := Open(path, []byte("oldpass")); !errors.Is(err, crypto.ErrAuthFailed) {
		t.Fatalf("old passphrase should fail, got %v", err)
	}
	// New passphrase opens, plaintext intact.
	got, _, _, err := Open(path, []byte("newpass"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pt) {
		t.Fatalf("payload changed across rekey")
	}
	// Recovery STILL works (this is the critical property).
	got2, _, _, err := OpenWithRecovery(path, rkek)
	if err != nil {
		t.Fatalf("recovery should still work after rekey: %v", err)
	}
	if !bytes.Equal(got2, pt) {
		t.Fatalf("recovery payload mismatch after rekey")
	}
	// .bak removed.
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf(".bak should be gone, got %v", err)
	}
}

func TestSaveV2RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v2.kpot")
	rkek := fakeRecoveryKEK(0x11)
	pt1 := []byte(`{"v":2,"notes":{"a":"1"}}`)
	pt2 := []byte(`{"v":2,"notes":{"a":"1","b":"2"}}`)

	dek, hdr, err := CreateV2WithRecovery(path, []byte("p"), WrapKindSeed, rkek, pt1)
	if err != nil {
		t.Fatal(err)
	}

	if err := Save(path, pt2, dek, hdr); err != nil {
		t.Fatal(err)
	}
	got, _, _, err := Open(path, []byte("p"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pt2) {
		t.Fatalf("save round-trip mismatch")
	}
	// Recovery still works after a Save.
	if _, _, _, err := OpenWithRecovery(path, rkek); err != nil {
		t.Fatalf("recovery broken after Save: %v", err)
	}
}

func TestOpenWithRecoveryFailsOnV1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v1.kpot")
	if _, _, err := Create(path, []byte("p"), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := OpenWithRecovery(path, fakeRecoveryKEK(0xAA)); !errors.Is(err, ErrNoRecovery) {
		t.Fatalf("expected ErrNoRecovery, got %v", err)
	}
}

func TestRekeyV2RejectsV1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v1.kpot")
	if _, _, err := Create(path, []byte("p"), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	dek := make([]byte, 32) // dummy
	if err := RekeyV2(path, dek, []byte("new")); err == nil {
		t.Fatal("expected error rejecting v1 vault")
	}
}

func TestRekeyV1RejectsV2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v2.kpot")
	if _, _, err := CreateV2WithRecovery(path, []byte("p"), WrapKindSeed, fakeRecoveryKEK(1), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := Rekey(path, []byte(`{}`), []byte("new")); err == nil {
		t.Fatal("expected Rekey to reject v2 vault")
	}
}
