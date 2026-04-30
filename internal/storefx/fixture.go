// Package storefx exposes vault fixtures for benchmarks and stress
// tests in other internal packages. It deliberately lives outside the
// `store` package so the fixture helpers don't pollute the public
// store API and don't ship in the kpot binary's symbol table.
//
// Anything in here is meant for tests/benches only — do not import
// from production code (cmd/, internal/serve/, etc.).
package storefx

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/Shin-R2un/kpot/internal/store"
)

// BuildLargeVault returns a *store.DecryptedVault populated with n
// synthetic notes whose names follow a 1Password-import-style pattern
// (`<group>/<slug>-<i>`) and whose bodies are ~1KB of random ASCII
// with a sprinkle of `key: value` rows. A fixed RNG seed keeps
// benchmark inputs reproducible across runs — relative comparisons
// between commits are what matter, not absolute numbers.
func BuildLargeVault(n int) *store.DecryptedVault {
	v := store.New()
	rng := rand.New(rand.NewSource(int64(n) ^ 0xc01dcaf3))

	groups := []string{"accounts", "ai", "dev", "server", "personal", "work", "social", "vendor"}
	slugs := []string{"foo", "bar", "baz", "qux", "alpha", "beta", "gamma", "delta", "epsilon"}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%s/%s-%d", groups[i%len(groups)], slugs[(i/len(groups))%len(slugs)], i)
		body := buildBody(rng, i)
		v.Notes[name] = &store.Note{
			Body:      body,
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
			UpdatedAt: now.Add(time.Duration(i) * time.Minute),
		}
	}
	v.UpdatedAt = now
	return v
}

func buildBody(rng *rand.Rand, idx int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# fixture-%d\n\n", idx)
	fmt.Fprintf(&sb, "url: https://example.com/path-%d\n", idx)
	fmt.Fprintf(&sb, "username: user-%d@example.com\n", idx)
	fmt.Fprintf(&sb, "apikey: sk-%s\n", randAlnum(rng, 32))
	fmt.Fprintf(&sb, "memo: %s\n\n", randAlnum(rng, 60))
	for j := 0; j < 8; j++ {
		fmt.Fprintf(&sb, "%s\n", randAlnum(rng, 80))
	}
	return sb.String()
}

func randAlnum(rng *rand.Rand, n int) string {
	const alpha = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = alpha[rng.Intn(len(alpha))]
	}
	return string(b)
}
