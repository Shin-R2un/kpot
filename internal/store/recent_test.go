package store

import (
	"reflect"
	"testing"
)

func TestTrackRecentDedupePushFront(t *testing.T) {
	v := New()
	for _, n := range []string{"a", "b", "c"} {
		if _, err := v.Put(n, "x"); err != nil {
			t.Fatal(err)
		}
	}

	v.TrackRecent("a")
	v.TrackRecent("b")
	v.TrackRecent("a") // re-touch a → should move to front

	want := []string{"a", "b"}
	if !reflect.DeepEqual(v.Recent, want) {
		t.Fatalf("Recent = %v, want %v", v.Recent, want)
	}
}

func TestTrackRecentTruncatesAtCap(t *testing.T) {
	v := New()
	// Generate RecentMax+5 distinct notes and track each.
	for i := 0; i < RecentMax+5; i++ {
		name := nameFor(i)
		if _, err := v.Put(name, "x"); err != nil {
			t.Fatal(err)
		}
		v.TrackRecent(name)
	}

	if got := len(v.Recent); got != RecentMax {
		t.Fatalf("len(Recent) = %d, want %d", got, RecentMax)
	}
	// Most-recent first. The last name pushed must be at index 0.
	wantHead := nameFor(RecentMax + 4)
	if v.Recent[0] != wantHead {
		t.Fatalf("Recent[0] = %q, want %q", v.Recent[0], wantHead)
	}
	// The first 5 names tracked must have fallen off the tail.
	for i := 0; i < 5; i++ {
		drop := nameFor(i)
		for _, kept := range v.Recent {
			if kept == drop {
				t.Fatalf("expected %q to be evicted but still present", drop)
			}
		}
	}
}

func TestTrackRecentNormalizesInput(t *testing.T) {
	v := New()
	if _, err := v.Put("ai/openai", "x"); err != nil {
		t.Fatal(err)
	}

	v.TrackRecent("AI/OpenAI") // mixed case → normalize → same canonical entry
	v.TrackRecent("ai/openai") // dedupe must collapse these

	if len(v.Recent) != 1 {
		t.Fatalf("len(Recent) = %d, want 1 after dedup", len(v.Recent))
	}
	if v.Recent[0] != "ai/openai" {
		t.Fatalf("Recent[0] = %q, want canonical 'ai/openai'", v.Recent[0])
	}
}

func TestTrackRecentSilentlyIgnoresInvalidName(t *testing.T) {
	v := New()
	v.TrackRecent("") // empty
	v.TrackRecent("/bad")
	v.TrackRecent("has space")
	if len(v.Recent) != 0 {
		t.Fatalf("Recent = %v, want empty (invalid names dropped)", v.Recent)
	}
}

func TestListRecentFiltersStale(t *testing.T) {
	v := New()
	for _, n := range []string{"a", "b", "c"} {
		if _, err := v.Put(n, "x"); err != nil {
			t.Fatal(err)
		}
		v.TrackRecent(n)
	}
	// Direct delete (bypassing PruneRecent) — simulate a note that
	// was rm'd via a different code path. ListRecent must still
	// hide it from the user.
	delete(v.Notes, "b")

	got := v.ListRecent()
	want := []string{"c", "a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListRecent = %v, want %v", got, want)
	}
}

func TestPruneRecentDropsStaleEntries(t *testing.T) {
	v := New()
	for _, n := range []string{"a", "b", "c"} {
		if _, err := v.Put(n, "x"); err != nil {
			t.Fatal(err)
		}
		v.TrackRecent(n)
	}
	delete(v.Notes, "a")
	delete(v.Notes, "c")

	v.PruneRecent()

	if !reflect.DeepEqual(v.Recent, []string{"b"}) {
		t.Fatalf("Recent after prune = %v, want [b]", v.Recent)
	}
}

// nameFor returns a deterministic canonical note name for index i.
// Used by recent / trash bench fixtures so tests don't share state
// with random vault generators.
func nameFor(i int) string {
	const a = "abcdefghijklmnopqrstuvwxyz"
	if i < len(a) {
		return string(a[i])
	}
	// Beyond 26 we just use digit suffix; canonicalisation is happy
	// with `n0`, `n1`, ... since digits are letters in the allowed set.
	return "n" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
