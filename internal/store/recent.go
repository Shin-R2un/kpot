package store

// RecentMax is the maximum number of entries kept in DecryptedVault.Recent.
// Older entries fall off the tail when TrackRecent prepends past this cap.
// Tuned for the "300+ note" daily-driver workflow — 20 fits a typical
// session's worth of distinct notes without bloating the encrypted payload.
const RecentMax = 20

// TrackRecent records that name was just accessed. The argument is
// canonicalised via NormalizeName; invalid names are silently dropped
// (callers always pass canon names from successful lookups, so this
// is defensive only).
//
// Behaviour:
//   - existing entry → moved to the front (de-duplicated)
//   - new entry      → prepended
//   - list capped at RecentMax (oldest entries fall off)
//
// Caller is responsible for persisting the vault — this method only
// mutates the in-memory slice.
func (v *DecryptedVault) TrackRecent(name string) {
	canon, err := NormalizeName(name)
	if err != nil {
		return
	}
	for i, n := range v.Recent {
		if n == canon {
			v.Recent = append(v.Recent[:i], v.Recent[i+1:]...)
			break
		}
	}
	v.Recent = append([]string{canon}, v.Recent...)
	if len(v.Recent) > RecentMax {
		v.Recent = v.Recent[:RecentMax]
	}
}

// ListRecent returns the recent-access list with stale entries (notes
// that no longer exist in v.Notes — e.g. trashed since access)
// filtered out. Order is preserved (most-recent first). The returned
// slice is a fresh copy; mutating it doesn't affect the vault.
func (v *DecryptedVault) ListRecent() []string {
	out := make([]string, 0, len(v.Recent))
	for _, n := range v.Recent {
		if _, ok := v.Notes[n]; ok {
			out = append(out, n)
		}
	}
	return out
}

// PruneRecent drops Recent entries that no longer exist in Notes. The
// REPL doesn't need to call this often (ListRecent already filters at
// read time), but Trash() invokes it so the persisted slice doesn't
// grow unboundedly with stale references after many rm cycles.
func (v *DecryptedVault) PruneRecent() {
	if len(v.Recent) == 0 {
		return
	}
	out := v.Recent[:0]
	for _, n := range v.Recent {
		if _, ok := v.Notes[n]; ok {
			out = append(out, n)
		}
	}
	v.Recent = out
}
