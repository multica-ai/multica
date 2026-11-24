// Package semver parses Semantic Versioning 2.0.0 versions and matches them
// against npm-style constraints.
package semver

type Version struct {
	Major, Minor, Patch int
	Pre                 []string
	Build               string
}

// Parse accepts an optional leading "v".
func Parse(s string) (Version, error) { panic("not implemented") }

func (v Version) String() string { panic("not implemented") }

// Compare returns -1, 0 or 1 by SemVer precedence; build metadata is ignored.
func Compare(a, b Version) int { panic("not implemented") }

// Match reports whether v satisfies constraint. Space-separated comparators
// (=, >, >=, <, <=, ^, ~, or a bare version) are ANDed, and "||" separates
// alternatives. A prerelease version only matches a group in which some
// comparator has a prerelease on the same major.minor.patch.
func Match(constraint string, v Version) (bool, error) { panic("not implemented") }
