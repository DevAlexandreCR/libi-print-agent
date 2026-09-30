package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed MAJOR.MINOR.PATCH[-PRERELEASE] version, deliberately
// simplified from full semver (build metadata is dropped, prerelease
// identifiers are compared as a single opaque string) since the agent only
// ever compares versions it built itself.
type Version struct {
	Major, Minor, Patch int
	Prerelease          string
}

// ParseVersion parses a version string, tolerating a leading "v" (as in
// git tags) and an optional "-prerelease" suffix; "+build" metadata, if
// present, is dropped before parsing.
func ParseVersion(s string) (Version, error) {
	orig := s
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}

	core := s
	var prerelease string
	if i := strings.IndexByte(s, '-'); i >= 0 {
		core = s[:i]
		prerelease = s[i+1:]
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("update: %q is not a MAJOR.MINOR.PATCH version", orig)
	}

	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("update: %q is not a MAJOR.MINOR.PATCH version", orig)
		}
		nums[i] = n
	}

	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2], Prerelease: prerelease}, nil
}

// Compare returns -1, 0 or 1 as a is less than, equal to, or greater than
// b. A version with a prerelease suffix is considered lower precedence
// than the same MAJOR.MINOR.PATCH without one (e.g. 1.2.0-beta < 1.2.0),
// matching semver's intent without implementing its full prerelease
// comparison algorithm.
func Compare(a, b Version) int {
	if c := compareInt(a.Major, b.Major); c != 0 {
		return c
	}
	if c := compareInt(a.Minor, b.Minor); c != 0 {
		return c
	}
	if c := compareInt(a.Patch, b.Patch); c != 0 {
		return c
	}
	switch {
	case a.Prerelease == "" && b.Prerelease == "":
		return 0
	case a.Prerelease == "" && b.Prerelease != "":
		return 1
	case a.Prerelease != "" && b.Prerelease == "":
		return -1
	default:
		return strings.Compare(a.Prerelease, b.Prerelease)
	}
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
