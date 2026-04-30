package store

import (
	"errors"
	"testing"
	"time"
)

func TestTrashRoundTripPreservesMetadata(t *testing.T) {
	v := New()
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	updated := created.Add(time.Hour)
	v.Notes["accounts/foo"] = &Note{Body: "hello", CreatedAt: created, UpdatedAt: updated}

	now := time.Date(2026, 5, 1, 15, 30, 12, 0, time.UTC)
	key, err := v.TrashNote("accounts/foo", now)
	if err != nil {
		t.Fatal(err)
	}
	if key != "accounts/foo.deleted-20260501-153012" {
		t.Errorf("trash key = %q, want timestamped name", key)
	}
	if _, ok := v.Notes["accounts/foo"]; ok {
		t.Errorf("note still in Notes after Trash")
	}
	t1 := v.Trash[key]
	if t1 == nil {
		t.Fatalf("trash entry %q absent", key)
	}
	if t1.OriginalName != "accounts/foo" || t1.Body != "hello" ||
		!t1.CreatedAt.Equal(created) || !t1.UpdatedAt.Equal(updated) ||
		!t1.DeletedAt.Equal(now) {
		t.Errorf("trash metadata mismatch: %+v", t1)
	}

	// Restore round-trip: body and timestamps come back identical.
	if err := v.Restore(key); err != nil {
		t.Fatal(err)
	}
	got, ok := v.Notes["accounts/foo"]
	if !ok {
		t.Fatalf("note not restored")
	}
	if got.Body != "hello" || !got.CreatedAt.Equal(created) || !got.UpdatedAt.Equal(updated) {
		t.Errorf("restored note mismatch: %+v", got)
	}
	if _, ok := v.Trash[key]; ok {
		t.Errorf("trash entry %q still present after Restore", key)
	}
}

func TestTrashSameNameSameSecondAppendsCounter(t *testing.T) {
	v := New()
	v.Notes["a"] = &Note{Body: "1"}
	v.Notes["a2"] = &Note{Body: "2"}

	now := time.Date(2026, 5, 1, 15, 30, 12, 0, time.UTC)
	k1, err := v.TrashNote("a", now)
	if err != nil {
		t.Fatal(err)
	}
	v.Notes["a"] = &Note{Body: "1b"} // recreate to be trashed at the same instant
	k2, err := v.TrashNote("a", now)
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k2 {
		t.Errorf("expected distinct trash keys, got both %q", k1)
	}
	if k2 != k1+"-2" {
		t.Errorf("second key = %q, want %q", k2, k1+"-2")
	}
}

func TestTrashMissingNote(t *testing.T) {
	v := New()
	_, err := v.TrashNote("does/not/exist", time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want wrapping ErrNotFound", err)
	}
}

func TestRestoreConflictRefuses(t *testing.T) {
	v := New()
	v.Notes["a"] = &Note{Body: "old"}
	now := time.Date(2026, 5, 1, 15, 0, 0, 0, time.UTC)
	key, err := v.TrashNote("a", now)
	if err != nil {
		t.Fatal(err)
	}
	// User creates a new note at the same name afterwards.
	v.Notes["a"] = &Note{Body: "new"}

	if err := v.Restore(key); err == nil {
		t.Fatal("expected restore conflict, got nil")
	}
	// Trash entry must still be there — restore failure shouldn't drop data.
	if _, ok := v.Trash[key]; !ok {
		t.Errorf("trash entry dropped on conflict; expected to be preserved")
	}
	// Live note must remain intact.
	if v.Notes["a"].Body != "new" {
		t.Errorf("live note overwritten by failed restore")
	}
}

func TestRestoreMissingKey(t *testing.T) {
	v := New()
	if err := v.Restore("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want wrapping ErrNotFound", err)
	}
}

func TestPurgeRemovesTrashEntry(t *testing.T) {
	v := New()
	v.Notes["a"] = &Note{Body: "x"}
	key, err := v.TrashNote("a", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Purge(key); err != nil {
		t.Fatal(err)
	}
	if _, ok := v.Trash[key]; ok {
		t.Errorf("trash entry %q still present after Purge", key)
	}
}

func TestPurgeMissingKey(t *testing.T) {
	v := New()
	if err := v.Purge("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want wrapping ErrNotFound", err)
	}
}

func TestPurgeAllEmptiesTrashAndReturnsCount(t *testing.T) {
	v := New()
	v.Notes["a"] = &Note{Body: "x"}
	v.Notes["b"] = &Note{Body: "y"}
	if _, err := v.TrashNote("a", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := v.TrashNote("b", time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	got := v.PurgeAll()
	if got != 2 {
		t.Errorf("PurgeAll = %d, want 2", got)
	}
	if len(v.Trash) != 0 {
		t.Errorf("Trash = %v, want empty", v.Trash)
	}
}

func TestListTrashSortedNewestFirst(t *testing.T) {
	v := New()
	v.Notes["a"] = &Note{Body: "1"}
	v.Notes["b"] = &Note{Body: "2"}
	v.Notes["c"] = &Note{Body: "3"}

	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	if _, err := v.TrashNote("a", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := v.TrashNote("b", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := v.TrashNote("c", t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	got := v.ListTrash()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Note.OriginalName != "c" || got[1].Note.OriginalName != "b" || got[2].Note.OriginalName != "a" {
		t.Errorf("order = %v, want c,b,a (newest first)",
			[]string{got[0].Note.OriginalName, got[1].Note.OriginalName, got[2].Note.OriginalName})
	}
}

func TestTrashPrunesRecent(t *testing.T) {
	v := New()
	for _, n := range []string{"a", "b", "c"} {
		v.Notes[n] = &Note{Body: "x"}
		v.TrackRecent(n)
	}
	if _, err := v.TrashNote("b", time.Now()); err != nil {
		t.Fatal(err)
	}

	for _, n := range v.Recent {
		if n == "b" {
			t.Errorf("Recent still contains trashed name %q: %v", n, v.Recent)
		}
	}
}
