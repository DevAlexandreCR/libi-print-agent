//go:build !windows

package config

import (
	"os"
	"path/filepath"
)

// Dir returns the config directory on non-Windows platforms. The agent only
// ships for Windows (see design.md Non-Goals), so this path only matters for
// `go test ./...` in the Linux build container and local development;
// override with LIBI_PRINT_AGENT_DIR to point at a test temp dir.
func Dir() (string, error) {
	if d := os.Getenv("LIBI_PRINT_AGENT_DIR"); d != "" {
		return d, nil
	}
	return filepath.Join(os.TempDir(), "libi-print-agent"), nil
}
