package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// VerifySHA256 checks that the file at path hashes to expectedHex (a lower-
// or upper-case hex-encoded sha256, as GET /print-agents/version reports
// it). A mismatch is returned as an error naming both hashes, never as a
// bool, so callers cannot accidentally ignore it.
func VerifySHA256(path, expectedHex string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("update: open %s for hashing: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("update: hash %s: %w", path, err)
	}
	got := hex.EncodeToString(h.Sum(nil))

	want := strings.ToLower(strings.TrimSpace(expectedHex))
	if got != want {
		return fmt.Errorf("update: sha256 mismatch for %s: got %s, want %s", path, got, want)
	}
	return nil
}
