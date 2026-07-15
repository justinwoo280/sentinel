package cli

import (
	"path/filepath"
)

// filepath_ returns the directory of a path (wraps filepath.Dir for
// convenience and to avoid naming conflicts).
func filepath_(p string) string {
	return filepath.Dir(p)
}
