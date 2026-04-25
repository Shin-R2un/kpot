package crypto

// Zero best-effort overwrites the byte slice with zeros.
// Go's GC may have already moved the underlying memory; this is not a
// strong guarantee, but reduces the window where secrets sit in heap.
func Zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
