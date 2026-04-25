package vault

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/r2un/kpot/internal/crypto"
)

func TestCreateOpenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.kpot")
	pass := []byte("hunter2")
	plaintext := []byte(`{"hello":"world","secret":"OPENAI_KEY=sk-xxx"}`)

	key, hdr, err := Create(path, pass, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(key)
	if hdr.Format != FormatName {
		t.Fatalf("format = %q", hdr.Format)
	}

	got, key2, hdr2, err := Open(path, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(key2)
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext mismatch: %q vs %q", got, plaintext)
	}
	if hdr2.KDF.Salt != hdr.KDF.Salt {
		t.Fatal("salt should be preserved across open")
	}
}

func TestNoPlaintextOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.kpot")
	pass := []byte("p")
	secret := "VERYSECRETMARKER12345"
	plaintext := []byte(`{"x":"` + secret + `"}`)

	_, _, err := Create(path, pass, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("vault file contains plaintext secret")
	}
}

func TestOpenWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.kpot")
	_, _, err := Create(path, []byte("right"), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Open(path, []byte("wrong")); err != crypto.ErrAuthFailed {
		t.Fatalf("expected ErrAuthFailed, got %v", err)
	}
}

func TestSaveCreatesBak(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.kpot")
	pass := []byte("p")

	_, _, err := Create(path, pass, []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}

	_, key, hdr, err := Open(path, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(key)

	if err := Save(path, []byte(`{"v":2}`), key, hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatal("expected .bak to be created")
	}
	got, _, _, err := Open(path, pass)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"v":2}` {
		t.Fatalf("got %q after save", got)
	}
}

func TestRefuseOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.kpot")
	if _, _, err := Create(path, []byte("p"), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Create(path, []byte("p"), []byte(`{}`)); err == nil {
		t.Fatal("expected error on existing file")
	}
}

func TestHeaderTamperDetected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.kpot")
	pass := []byte("p")
	if _, _, err := Create(path, pass, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	var h Header
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatal(err)
	}
	h.KDF.Params.Iterations = 1
	tampered, _ := json.MarshalIndent(&h, "", "  ")
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := Open(path, pass); err != crypto.ErrAuthFailed {
		t.Fatalf("expected ErrAuthFailed for tampered header, got %v", err)
	}
}
