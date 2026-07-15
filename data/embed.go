// Package data embeds all static data files (map, UAs, keywords,
// regions) into the binary. Other packages read from FS.
package data

import "embed"

// FS is the embedded filesystem containing all static data.
//
// regions is a bare directory pattern (no glob suffix), which embeds the
// entire subtree recursively — required for the 4-level
// regions/<country>/<state>/<city>.json hierarchy.
//
//go:embed user_agents.txt map.json
//go:embed keywords/*.txt
//go:embed regions
var FS embed.FS
