package ctrl

import "bytes"

// newBoundedReader returns a reader over raw. Callers must already have
// enforced MaxMessageBytes on len(raw); this indirection keeps the
// decode path swappable (e.g. to a streaming limited reader later).
func newBoundedReader(raw []byte) *bytes.Reader {
	return bytes.NewReader(raw)
}
