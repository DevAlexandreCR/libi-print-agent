package update

import "github.com/libi/libi-print-agent/internal/api"

// ShouldUpdate decides whether info describes a build worth installing over
// currentVersion. It is pure (no I/O) so the decision is unit-testable
// without a network or a real binary:
//   - info == nil or info.Version == nil (the {"version":null} shape the
//     API returns before any release is published) -> false, no error.
//   - info.Version or currentVersion fails to parse as MAJOR.MINOR.PATCH
//     -> false, error (a dev build's "dev" version never "updates").
//   - otherwise -> true iff the remote version compares greater.
func ShouldUpdate(currentVersion string, info *api.VersionInfo) (bool, error) {
	if info == nil || info.Version == nil {
		return false, nil
	}

	current, err := ParseVersion(currentVersion)
	if err != nil {
		return false, err
	}
	remote, err := ParseVersion(*info.Version)
	if err != nil {
		return false, err
	}

	return Compare(remote, current) > 0, nil
}
