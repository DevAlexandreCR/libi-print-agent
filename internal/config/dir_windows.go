//go:build windows

package config

import (
	"os"
	"path/filepath"
)

// Dir returns %ProgramData%\LiBi, overridable via LIBI_PRINT_AGENT_DIR (used
// by tests and local debugging).
func Dir() (string, error) {
	if d := os.Getenv("LIBI_PRINT_AGENT_DIR"); d != "" {
		return d, nil
	}
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "LiBi"), nil
}
