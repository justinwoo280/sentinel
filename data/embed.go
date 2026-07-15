// Package data embeds all static data files (map, UAs, keywords,
// regions) into the binary. Other packages read from FS.
package data

import "embed"

// FS is the embedded filesystem containing all static data.
//
//go:embed user_agents.txt map.json
//go:embed keywords/*.txt
//go:embed regions/*.json
var FS embed.FS
