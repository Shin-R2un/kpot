package store_test

import (
	"strings"
	"testing"

	"github.com/Shin-R2un/kpot/internal/storefx"
)

// BenchmarkFind_1000Notes measures full-text search latency over a
// realistic 1000-note vault. The query string ("apikey") matches every
// note's body so we exercise the worst-case scan (no early-exit
// shortcuts). Real-world queries hit fewer notes and are correspondingly
// faster.
func BenchmarkFind_1000Notes(b *testing.B) {
	v := storefx.BuildLargeVault(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Find("apikey")
	}
}

// BenchmarkSetField_1000Notes measures in-memory field update on a
// large vault. This is the per-keystroke cost the user pays when
// running `set <field>` — it should be microseconds even for the
// biggest realistic vault.
//
// Note: this measures the in-memory mutation (Put → JSON re-marshal
// happens at persist time, which is benchmarked separately by
// BenchmarkSave in vault/io_bench_test.go).
func BenchmarkSetField_1000Notes(b *testing.B) {
	v := storefx.BuildLargeVault(1000)
	target := "accounts/foo-0"
	if _, ok := v.Notes[target]; !ok {
		b.Fatalf("fixture missing %q", target)
	}
	body := v.Notes[target].Body
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Toggle the apikey value via field-set semantics. Use a
		// simple find/replace stand-in to avoid pulling fields/...
		// dependencies into the store package — a real `set` runs
		// fields.Set but the store-layer cost is the Put + body
		// replace.
		newBody := strings.Replace(body, "apikey: ", "apikey-x: ", 1)
		v.Notes[target].Body = newBody
		body = newBody
	}
}

// BenchmarkTrackRecent measures the Recent-list maintenance cost.
// Even with a full 20-entry list, dedupe + prepend should be sub-µs
// — vault persist is the actual cost of `cd`, not this.
func BenchmarkTrackRecent(b *testing.B) {
	v := storefx.BuildLargeVault(1000)
	names := make([]string, 0, 30)
	for n := range v.Notes {
		names = append(names, n)
		if len(names) == 30 {
			break
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.TrackRecent(names[i%len(names)])
	}
}
