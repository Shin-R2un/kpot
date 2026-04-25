package tty

import (
	"sync"
	"testing"
)

func TestReadPassphraseUsesEnv(t *testing.T) {
	// Reset the once so we can assert the warning fires.
	envWarnOnce = sync.Once{}
	t.Setenv(PassphraseEnv, "secret-from-env")

	got, err := ReadPassphrase("Passphrase: ")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "secret-from-env" {
		t.Fatalf("got %q, want %q", string(got), "secret-from-env")
	}
}

func TestReadPassphraseEnvOverridesEvenEmpty(t *testing.T) {
	envWarnOnce = sync.Once{}
	t.Setenv(PassphraseEnv, "")

	got, err := ReadPassphrase("Passphrase: ")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "" {
		t.Fatalf("env-empty should return empty, got %q", string(got))
	}
}
