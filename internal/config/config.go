// Package config loads and saves the agent's on-disk configuration
// (%ProgramData%\LiBi\agent.json on Windows). The bearer token is the only
// secret it holds and is encrypted at rest via a Protector (DPAPI in
// production, see protector_windows.go) so it survives Windows user logouts
// while resisting casual disk inspection.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// fileName is the config file's name inside Dir().
const fileName = "agent.json"

// Config is the agent's in-memory configuration. Token is always the
// decrypted value; on disk it is stored encrypted (see fileConfig).
type Config struct {
	APIBaseURL string
	Token      string
	AgentID    string
	Name       string
}

// Paired reports whether the agent has completed pairing (task 8.3).
func (c *Config) Paired() bool {
	return c.AgentID != "" && c.Token != ""
}

// fileConfig is the on-disk JSON shape. Field names match design.md D4/D8.
type fileConfig struct {
	APIBaseURL     string `json:"apiBaseUrl"`
	EncryptedToken []byte `json:"encryptedToken,omitempty"`
	AgentID        string `json:"agentId,omitempty"`
	Name           string `json:"name,omitempty"`
}

// Protector encrypts/decrypts the token at rest. Implemented by DPAPI on
// Windows (protector_windows.go) and by PlainProtector for tests.
type Protector interface {
	Protect(plaintext []byte) ([]byte, error)
	Unprotect(ciphertext []byte) ([]byte, error)
}

// Path returns the full path to the config file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load reads the config file, decrypting the token with p. A missing file is
// not an error: it means the agent is not yet paired, and Load returns a
// zero-value Config.
func Load(p Protector) (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	cfg := &Config{
		APIBaseURL: fc.APIBaseURL,
		AgentID:    fc.AgentID,
		Name:       fc.Name,
	}
	if len(fc.EncryptedToken) > 0 {
		plain, err := p.Unprotect(fc.EncryptedToken)
		if err != nil {
			return nil, fmt.Errorf("config: decrypt token: %w", err)
		}
		cfg.Token = string(plain)
	}
	return cfg, nil
}

// Save writes the config file, encrypting the token with p. Directory
// creation and the write are best-effort atomic (write to a temp file, then
// rename) so a crash mid-write cannot leave a half-written file behind.
func Save(cfg *Config, p Protector) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: create %s: %w", dir, err)
	}

	fc := fileConfig{
		APIBaseURL: cfg.APIBaseURL,
		AgentID:    cfg.AgentID,
		Name:       cfg.Name,
	}
	if cfg.Token != "" {
		enc, err := p.Protect([]byte(cfg.Token))
		if err != nil {
			return fmt.Errorf("config: encrypt token: %w", err)
		}
		fc.EncryptedToken = enc
	}

	data, err := json.MarshalIndent(fc, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}

	path := filepath.Join(dir, fileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("config: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("config: rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}
