package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFileReturnsEmptyConfig(t *testing.T) {
	t.Setenv("LIBI_PRINT_AGENT_DIR", t.TempDir())

	cfg, err := Load(PlainProtector{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Paired() {
		t.Fatalf("expected an unpaired config, got %+v", cfg)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("LIBI_PRINT_AGENT_DIR", t.TempDir())

	want := &Config{
		APIBaseURL: "https://api.libi.example/api",
		Token:      "super-secret-agent-token",
		AgentID:    "agent-123",
		Name:       "Caja",
	}

	if err := Save(want, PlainProtector{}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(PlainProtector{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if *got != *want {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, want)
	}
	if !got.Paired() {
		t.Fatalf("expected Paired() to be true after round trip")
	}
}

func TestSaveDoesNotStoreTokenInPlaintextOnDisk(t *testing.T) {
	t.Setenv("LIBI_PRINT_AGENT_DIR", t.TempDir())

	secret := "super-secret-agent-token"
	cfg := &Config{APIBaseURL: "https://api.libi.example/api", Token: secret, AgentID: "a1"}

	if err := Save(cfg, dpapiLikeProtector{}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	path, err := Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("token stored in plaintext on disk: %s", raw)
	}
}

func TestDirHonorsOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom")
	t.Setenv("LIBI_PRINT_AGENT_DIR", want)

	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	if got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
}

// dpapiLikeProtector is a reversible but non-identity Protector, so the
// plaintext-on-disk test above is meaningful (PlainProtector would trivially
// "contain" the secret).
type dpapiLikeProtector struct{}

func (dpapiLikeProtector) Protect(data []byte) ([]byte, error) {
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ 0xAA
	}
	return out, nil
}

func (dpapiLikeProtector) Unprotect(data []byte) ([]byte, error) {
	return dpapiLikeProtector{}.Protect(data) // XOR is its own inverse
}
