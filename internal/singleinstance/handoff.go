package singleinstance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// handoffFileName is the file inside the config directory that the running
// instance publishes its local status-page address to.
const handoffFileName = "ui.json"

// Handoff is the running instance's local status-page address: enough for
// a second launch to open the same page in the browser instead of starting
// a second agent.
type Handoff struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// URL returns the status page's localhost URL, token included, ready to
// hand to the default browser.
func (h Handoff) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/?t=%s", h.Port, h.Token)
}

// WriteHandoff publishes h to dir, so a second launch can find the running
// instance. Best-effort atomic (write then rename), matching config.Save.
func WriteHandoff(dir string, h Handoff) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("singleinstance: create %s: %w", dir, err)
	}
	data, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("singleinstance: marshal handoff: %w", err)
	}
	path := filepath.Join(dir, handoffFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("singleinstance: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("singleinstance: rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// ReadHandoff reads the handoff file written by the currently running
// instance. A missing or corrupt file is reported as an error: callers
// (the second launch) fall back to just starting normally in that case,
// since there is evidently nothing usable to hand off to.
func ReadHandoff(dir string) (Handoff, error) {
	path := filepath.Join(dir, handoffFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return Handoff{}, fmt.Errorf("singleinstance: read %s: %w", path, err)
	}
	var h Handoff
	if err := json.Unmarshal(data, &h); err != nil {
		return Handoff{}, fmt.Errorf("singleinstance: parse %s: %w", path, err)
	}
	if h.Port <= 0 || h.Port > 65535 || h.Token == "" {
		return Handoff{}, fmt.Errorf("singleinstance: %s: invalid handoff %+v", path, h)
	}
	return h, nil
}

// RemoveHandoff deletes the handoff file on clean shutdown, so a later
// second-launch does not try to hand off to a process that is no longer
// running. Missing-file is not an error.
func RemoveHandoff(dir string) error {
	err := os.Remove(filepath.Join(dir, handoffFileName))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
