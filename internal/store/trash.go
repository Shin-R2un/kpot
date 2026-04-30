package store

import (
	"fmt"
	"sort"
	"time"
)

// TrashTimeFormat is the timestamp suffix appended to trashed note keys:
// `<original-name>.deleted-YYYYMMDD-HHMMSS`. Sortable lexicographically
// (so map iteration → sort = time order) and unambiguous for humans.
const TrashTimeFormat = "20060102-150405"

// TrashNote moves the note named `name` from Notes into Trash. The
// trash key is `<canon>.deleted-<TrashTimeFormat(now)>`; when the same
// note is trashed twice in the same second, a `-2`, `-3`, ... suffix
// is appended so we never silently overwrite an earlier trash entry.
//
// Returns the trash key on success so the REPL can echo it back to the
// user (`moved to trash: foo.deleted-20260501-153012`). Recent is
// pruned of the just-trashed name as a side effect.
//
// Method is named TrashNote (not Trash) because the Trash field on
// DecryptedVault holds the trash map; Go forbids a method and a
// field with the same name on the same receiver.
func (v *DecryptedVault) TrashNote(name string, now time.Time) (string, error) {
	canon, err := NormalizeName(name)
	if err != nil {
		return "", err
	}
	note, ok := v.Notes[canon]
	if !ok {
		return "", fmt.Errorf("note %q: %w", canon, ErrNotFound)
	}
	if v.Trash == nil {
		v.Trash = map[string]*TrashedNote{}
	}
	base := canon + ".deleted-" + now.UTC().Format(TrashTimeFormat)
	key := base
	for i := 2; ; i++ {
		if _, exists := v.Trash[key]; !exists {
			break
		}
		key = fmt.Sprintf("%s-%d", base, i)
	}
	v.Trash[key] = &TrashedNote{
		OriginalName: canon,
		Body:         note.Body,
		CreatedAt:    note.CreatedAt,
		UpdatedAt:    note.UpdatedAt,
		DeletedAt:    now.UTC(),
	}
	delete(v.Notes, canon)
	v.PruneRecent()
	return key, nil
}

// Restore moves the trashed note keyed by trashName back into Notes
// under its OriginalName. If a live note already exists at that name
// (created after the trash), Restore refuses rather than overwriting —
// the caller can rename the conflicting note first and try again.
//
// On success the trash entry is removed.
func (v *DecryptedVault) Restore(trashName string) error {
	t, ok := v.Trash[trashName]
	if !ok {
		return fmt.Errorf("trash entry %q: %w", trashName, ErrNotFound)
	}
	if _, exists := v.Notes[t.OriginalName]; exists {
		return fmt.Errorf("note %q already exists; rename the live note before restoring", t.OriginalName)
	}
	v.Notes[t.OriginalName] = &Note{
		Body:      t.Body,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
	delete(v.Trash, trashName)
	return nil
}

// Purge permanently removes the trash entry keyed by trashName. Live
// Notes are untouched. Returns ErrNotFound when no such entry exists.
func (v *DecryptedVault) Purge(trashName string) error {
	if _, ok := v.Trash[trashName]; !ok {
		return fmt.Errorf("trash entry %q: %w", trashName, ErrNotFound)
	}
	delete(v.Trash, trashName)
	return nil
}

// PurgeAll empties the entire trash and returns the number of entries
// removed. Live Notes are untouched. Safe to call on an empty Trash.
func (v *DecryptedVault) PurgeAll() int {
	n := len(v.Trash)
	v.Trash = nil
	return n
}

// ListTrash returns trash entries sorted by DeletedAt descending
// (newest first). Each pair carries the trash key (the user passes
// this back to Restore / Purge) plus the snapshot. Slice is fresh —
// callers can sort/filter without affecting the vault.
type TrashEntry struct {
	Key  string
	Note *TrashedNote
}

func (v *DecryptedVault) ListTrash() []TrashEntry {
	out := make([]TrashEntry, 0, len(v.Trash))
	for k, t := range v.Trash {
		out = append(out, TrashEntry{Key: k, Note: t})
	}
	sort.Slice(out, func(i, j int) bool {
		ai, aj := out[i].Note.DeletedAt, out[j].Note.DeletedAt
		if ai.Equal(aj) {
			return out[i].Key < out[j].Key
		}
		return ai.After(aj)
	})
	return out
}
