package update

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/libi/libi-print-agent/internal/api"
)

// VersionChecker is the subset of *api.Client the updater needs (pair/
// heartbeat/jobs are irrelevant here), so tests can supply a stub instead
// of a real HTTP round trip for the version check itself.
type VersionChecker interface {
	Version(ctx context.Context) (*api.VersionInfo, error)
}

// Updater checks for and applies newer agent builds (design.md D8, task
// 8.4's self-update requirement). Zero value is not usable; construct with
// fields set as shown in NewUpdater.
type Updater struct {
	Client           VersionChecker
	HTTPClient       *http.Client
	CurrentVersion   string
	ExePath          string // the running executable's own path, to swap
	RequireSignature bool
	Logger           *slog.Logger
	IsBusy           func() bool                // true while a print job is in flight; skip this cycle
	Relaunch         func(exePath string) error // starts the new binary; overridable in tests
	VerifySignature  func(path string) error    // defaults to VerifyAuthenticode; overridable in tests
}

// NewUpdater returns an Updater with its function fields defaulted to the
// real implementations (a real HTTP download, the real Authenticode check,
// a real process relaunch).
func NewUpdater(client VersionChecker, httpClient *http.Client, currentVersion, exePath string, requireSignature bool, isBusy func() bool, logger *slog.Logger) *Updater {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Updater{
		Client:           client,
		HTTPClient:       httpClient,
		CurrentVersion:   currentVersion,
		ExePath:          exePath,
		RequireSignature: requireSignature,
		Logger:           logger,
		IsBusy:           isBusy,
		Relaunch:         defaultRelaunch,
		VerifySignature:  VerifyAuthenticode,
	}
}

func defaultRelaunch(exePath string) error {
	return exec.Command(exePath).Start()
}

// CleanupStaleBinary removes exePath+".old" left behind by a previous
// successful swap (see CheckAndApply), best-effort. Call this once at
// startup, before the first CheckAndApply.
func CleanupStaleBinary(exePath string) {
	_ = os.Remove(exePath + ".old")
}

// CheckAndApply runs one check-and-maybe-update cycle: never returns an
// error the caller must act on (every failure is logged and leaves the
// current version running, per design.md's "any failure logs and keeps the
// current version" requirement); applied reports whether a new binary was
// swapped in and relaunched, in which case the caller must exit this
// process immediately.
func (u *Updater) CheckAndApply(ctx context.Context) (applied bool) {
	info, err := u.Client.Version(ctx)
	if err != nil {
		u.Logger.Warn("update: version check failed", "error", err)
		return false
	}

	should, err := ShouldUpdate(u.CurrentVersion, info)
	if err != nil {
		u.Logger.Warn("update: could not compare versions", "error", err)
		return false
	}
	if !should {
		return false
	}

	if u.IsBusy != nil && u.IsBusy() {
		u.Logger.Info("update: newer version available but a print job is in flight, will retry next cycle", "remoteVersion", *info.Version)
		return false
	}

	u.Logger.Info("update: newer version available, downloading", "currentVersion", u.CurrentVersion, "remoteVersion", *info.Version)
	if err := u.apply(ctx, info); err != nil {
		u.Logger.Error("update: failed, staying on current version", "error", err)
		return false
	}
	return true
}

func (u *Updater) apply(ctx context.Context, info *api.VersionInfo) error {
	dir := filepath.Dir(u.ExePath)
	tmp, err := os.CreateTemp(dir, "libi-print-agent-update-*.exe.tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if err := u.download(ctx, info.URL, tmp); err != nil {
		tmp.Close()
		return fmt.Errorf("download %s: %w", info.URL, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close downloaded file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod downloaded file: %w", err)
	}

	if err := VerifySHA256(tmpPath, info.SHA256); err != nil {
		return err
	}

	if u.RequireSignature {
		verify := u.VerifySignature
		if verify == nil {
			verify = VerifyAuthenticode
		}
		if err := verify(tmpPath); err != nil {
			return err
		}
	} else {
		u.Logger.Warn("update: skipping Authenticode verification (RequireSignature=false build)")
	}

	if err := u.swap(tmpPath); err != nil {
		return err
	}

	relaunch := u.Relaunch
	if relaunch == nil {
		relaunch = defaultRelaunch
	}
	if err := relaunch(u.ExePath); err != nil {
		return fmt.Errorf("relaunch %s: %w", u.ExePath, err)
	}
	u.Logger.Info("update: applied and relaunched", "version", *info.Version)
	return nil
}

func (u *Updater) download(ctx context.Context, url string, dst *os.File) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	_, err = io.Copy(dst, resp.Body)
	return err
}

// swap renames the running executable aside (Windows permits renaming a
// file that is currently mapped/executing) and moves the verified new
// binary into its place, rolling back on failure so a partial swap never
// leaves the agent unable to start.
func (u *Updater) swap(newPath string) error {
	oldPath := u.ExePath + ".old"
	_ = os.Remove(oldPath) // clear any stale .old from an interrupted previous swap

	if err := os.Rename(u.ExePath, oldPath); err != nil {
		return fmt.Errorf("rename running binary aside: %w", err)
	}
	if err := os.Rename(newPath, u.ExePath); err != nil {
		if rollbackErr := os.Rename(oldPath, u.ExePath); rollbackErr != nil {
			return fmt.Errorf("move new binary into place failed (%v) AND rollback failed (%w): agent may not start", err, rollbackErr)
		}
		return fmt.Errorf("move new binary into place: %w (rolled back)", err)
	}
	return nil
}
