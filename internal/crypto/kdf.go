package crypto

import (
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
)

const (
	SaltSize = 16
	KeySize  = 32
)

type Argon2idParams struct {
	MemoryKiB   uint32 `json:"memory_kib"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
}

func DefaultArgon2idParams() Argon2idParams {
	return Argon2idParams{
		MemoryKiB:   64 * 1024,
		Iterations:  3,
		Parallelism: 1,
	}
}

func (p Argon2idParams) Validate() error {
	if p.MemoryKiB < 8*1024 {
		return fmt.Errorf("argon2id memory too low: %d KiB", p.MemoryKiB)
	}
	if p.Iterations == 0 {
		return errors.New("argon2id iterations must be > 0")
	}
	if p.Parallelism == 0 {
		return errors.New("argon2id parallelism must be > 0")
	}
	return nil
}

func NewSalt() ([]byte, error) {
	s := make([]byte, SaltSize)
	if _, err := rand.Read(s); err != nil {
		return nil, fmt.Errorf("salt generation: %w", err)
	}
	return s, nil
}

func DeriveKey(passphrase, salt []byte, p Argon2idParams) []byte {
	return argon2.IDKey(passphrase, salt, p.Iterations, p.MemoryKiB, p.Parallelism, KeySize)
}
