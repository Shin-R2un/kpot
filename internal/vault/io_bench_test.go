package vault_test

import (
	"path/filepath"
	"testing"

	"github.com/Shin-R2un/kpot/internal/storefx"
	"github.com/Shin-R2un/kpot/internal/vault"
)

// BenchmarkOpenVault_100Notes / _1000Notes measure the cost of
// passphrase-based open, which is dominated by Argon2id. Vault size
// affects only the trailing payload-decode step (microseconds compared
// to the KDF). Useful as a baseline so we notice if a future vault
// format change inflates the fixed Argon2id cost.
func BenchmarkOpenVault_100Notes(b *testing.B) {
	benchOpenVault(b, 100)
}

func BenchmarkOpenVault_1000Notes(b *testing.B) {
	benchOpenVault(b, 1000)
}

// BenchmarkSave_1000Notes measures the cost of re-encrypting and
// atomically writing the entire vault. This is the per-keystroke cost
// users pay when running `set` / `cd` / `cp` (each persists Recent
// updates). Aim is sub-100ms on a typical CPU; if it slips, the
// REPL UX of "every navigation persists" needs revisiting.
func BenchmarkSave_1000Notes(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "v.kpot")
	v := storefx.BuildLargeVault(1000)
	plaintext, err := v.ToJSON()
	if err != nil {
		b.Fatal(err)
	}

	pass := []byte("benchmark-passphrase-not-secret")
	key, hdr, err := vault.Create(path, pass, plaintext)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := vault.Save(path, plaintext, key, hdr); err != nil {
			b.Fatal(err)
		}
	}
}

func benchOpenVault(b *testing.B, n int) {
	dir := b.TempDir()
	path := filepath.Join(dir, "v.kpot")
	v := storefx.BuildLargeVault(n)
	plaintext, err := v.ToJSON()
	if err != nil {
		b.Fatal(err)
	}
	pass := []byte("benchmark-passphrase-not-secret")
	if _, _, err := vault.Create(path, pass, plaintext); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Each iteration re-derives the KEK from the passphrase via
		// Argon2id — that's the dominant cost we're measuring.
		_, _, _, err := vault.Open(path, pass)
		if err != nil {
			b.Fatal(err)
		}
	}
}
