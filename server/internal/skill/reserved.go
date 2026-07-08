package skill

import (
	"path/filepath"
	"strings"
)

// ContentFilename is the canonical filename of a skill's primary content
// (the Content field). The daemon writes Content to this path itself when
// preparing the execution environment, so it is reserved: a supporting file
// may not also claim it.
const ContentFilename = "SKILL.md"

// IsReservedContentPath reports whether p targets the reserved primary
// content file (SKILL.md).
//
// The path is cleaned before comparison so non-canonical spellings like
// "./SKILL.md" or "sub/../SKILL.md" — which filepath.Join still resolves onto
// the very SKILL.md the daemon writes itself — are caught too. An exact
// string match would let them slip through both the API guards and the daemon
// guard, and the duplicate write would then fail task prep with
// errPathPreExists (or, on the nil-manifest path, clobber the primary
// content). Comparison is case-insensitive to match the rest of the SKILL.md
// handling in this package and the daemon.
func IsReservedContentPath(p string) bool {
	return strings.EqualFold(filepath.Clean(p), ContentFilename)
}
