package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(path, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// sha256("hello world")
	const wantHex = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

	if err := VerifySHA256(path, wantHex); err != nil {
		t.Fatalf("VerifySHA256() with correct hash error = %v", err)
	}
	// Case-insensitivity.
	if err := VerifySHA256(path, "B94D27B9934D3E08A52E52D7DA7DABFAC484EFE37A5380EE9088F7ACE2EFCDE9"); err != nil {
		t.Fatalf("VerifySHA256() with uppercase hash error = %v", err)
	}

	if err := VerifySHA256(path, "0000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("VerifySHA256() with wrong hash expected error, got nil")
	}

	if err := VerifySHA256(filepath.Join(dir, "missing.bin"), wantHex); err == nil {
		t.Fatal("VerifySHA256() for missing file expected error, got nil")
	}
}
