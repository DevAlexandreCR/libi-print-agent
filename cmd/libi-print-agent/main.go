// Command libi-print-agent is the LiBi print agent: a small Windows tray
// process that receives print jobs from the LiBi API and writes them as RAW
// ESC/POS to a named Windows printer through the spooler (design.md D1/D2).
//
// Task 8.2 added the winspool RAW printing/enumeration backend
// (internal/spool). Task 8.3 added the API client and the print loop
// (internal/api, internal/agent). Task 8.4 adds the rest of the desktop
// experience: the tray icon and the local status/pairing page
// (internal/tray, internal/ui), relocating to a stable, admin-free install
// location and registering startup at logon (internal/install,
// internal/autostart), single-instance enforcement so a second launch hands
// off instead of duplicating every print (internal/singleinstance), and the
// self-update check/apply loop (internal/update). The hidden -pair/-api
// flags remain for support/testing: they pre-authenticate before the normal
// startup sequence runs, rather than replacing it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"github.com/libi/libi-print-agent/internal/agent"
	"github.com/libi/libi-print-agent/internal/api"
	"github.com/libi/libi-print-agent/internal/autostart"
	"github.com/libi/libi-print-agent/internal/browser"
	"github.com/libi/libi-print-agent/internal/config"
	"github.com/libi/libi-print-agent/internal/install"
	"github.com/libi/libi-print-agent/internal/logging"
	"github.com/libi/libi-print-agent/internal/singleinstance"
	"github.com/libi/libi-print-agent/internal/spool"
	"github.com/libi/libi-print-agent/internal/tray"
	"github.com/libi/libi-print-agent/internal/ui"
	"github.com/libi/libi-print-agent/internal/update"
)

// version, defaultAPIBase and requireSignature are set at build time via
// -ldflags "-X main.xxx=...". requireSignature is a string (ldflags can
// only set string vars) parsed to a bool below; it defaults to "true" so a
// build that forgets to set the flag fails safe and still checks
// Authenticode signatures (design.md D8/D12).
var (
	version          = "dev"
	defaultAPIBase   = ""
	requireSignature = "true"
)

// updateCheckInterval is a var, not a const, so a test could shrink it;
// none does yet, but this matches the pattern used for the agent package's
// own tunables (internal/agent/runner.go).
var updateCheckInterval = 6 * time.Hour

func main() {
	if handled, err := runDiagnostics(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintf(os.Stderr, "libi-print-agent: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "libi-print-agent: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("libi-print-agent", flag.ContinueOnError)
	pairCode := fs.String("pair", "", "hidden: redeem this pairing code before starting (testing/support only)")
	apiURL := fs.String("api", "", "API base URL to use with -pair, overriding the compiled-in default")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir, err := config.Dir()
	if err != nil {
		return fmt.Errorf("resolve config dir: %w", err)
	}

	logger, logCloser, err := logging.New(filepath.Join(dir, "logs"))
	if err != nil {
		return fmt.Errorf("set up logging: %w", err)
	}
	defer logCloser.Close()
	slog.SetDefault(logger)

	// Relocate to the stable, admin-free, self-update-writable install
	// location on first run; a no-op on non-Windows and once already
	// there. If it relaunches a new process from that location, this one's
	// job is done: it has taken no lock and holds no resources to clean up.
	relaunched, err := install.EnsureInstalled(logger)
	if err != nil {
		logger.Warn("self-install failed, continuing from the current location", "error", err)
	}
	if relaunched {
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		logger.Warn("could not resolve running executable path; self-update is disabled this run", "error", err)
		exePath = ""
	} else {
		update.CleanupStaleBinary(exePath)
	}

	// Single-instance: from here on we are running from the stable
	// location, so this is the one dedup point that matters regardless of
	// how many copies of the original file were double-clicked. A second
	// launch hands off to the running instance's status page instead of
	// starting a second agent, which would duplicate every print.
	lock, acquired, lockErr := singleinstance.Acquire()
	if lockErr != nil {
		logger.Warn("single-instance check failed, continuing anyway", "error", lockErr)
	} else if !acquired {
		if h, herr := singleinstance.ReadHandoff(dir); herr == nil {
			logger.Info("another instance is already running; opening its status page")
			_ = browser.Open(h.URL())
		} else {
			logger.Warn("another instance holds the single-instance lock but its handoff file is unreadable", "error", herr)
		}
		return nil
	}
	if lock != nil {
		defer lock.Release()
	}

	cfg, err := config.Load(config.NewDPAPIProtector())
	if err != nil {
		logger.Error("failed to load config", "error", err)
		return err
	}

	logger.Info("libi-print-agent starting",
		"version", version,
		"paired", cfg.Paired(),
		"configDir", dir,
	)

	printer := spool.New()
	runner, err := agent.NewRunner(dir, cfg, config.NewDPAPIProtector(), printer, &http.Client{}, version, logger)
	if err != nil {
		return fmt.Errorf("set up agent: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	apiBase := defaultAPIBase
	if cfg.APIBaseURL != "" {
		apiBase = cfg.APIBaseURL
	}

	if *pairCode != "" {
		pairAPIBase := *apiURL
		if pairAPIBase == "" {
			pairAPIBase = apiBase
		}
		if pairAPIBase == "" {
			return errors.New("libi-print-agent: -pair requires -api (no compiled-in default API base)")
		}
		if err := runner.Pair(ctx, pairAPIBase, *pairCode, ""); err != nil {
			return fmt.Errorf("pair: %w", err)
		}
		apiBase = pairAPIBase
	}

	uiServer, err := ui.New(runnerUIAdapter{runner}, apiBase, logger)
	if err != nil {
		return fmt.Errorf("set up status page: %w", err)
	}
	ln, err := uiServer.Listen()
	if err != nil {
		return fmt.Errorf("listen for status page: %w", err)
	}
	go func() {
		if err := uiServer.Serve(ln); err != nil {
			logger.Error("status page server stopped unexpectedly", "error", err)
		}
	}()

	if err := singleinstance.WriteHandoff(dir, singleinstance.Handoff{Port: uiServer.Port(), Token: uiServer.Token()}); err != nil {
		logger.Warn("could not write single-instance handoff file; a second launch will not find this one", "error", err)
	} else {
		defer singleinstance.RemoveHandoff(dir)
	}

	if !runner.Status().Paired {
		logger.Info("agent is not paired; opening the status page in the default browser")
		_ = browser.Open(uiServer.URL())
	}

	if exePath != "" {
		go func() {
			if err := autostart.Ensure(exePath, logger); err != nil {
				logger.Warn("startup-at-logon registration failed", "error", err)
			}
		}()
	}

	if exePath != "" {
		versionClient := api.NewClient(apiBase, &http.Client{})
		requireSig := requireSignature != "false"
		updater := update.NewUpdater(versionClient, &http.Client{}, version, exePath, requireSig, runner.Busy, logger)
		go runUpdateLoop(ctx, updater, stop)
	} else {
		logger.Warn("self-update is disabled: running executable path is unknown")
	}

	// The tray's message loop is thread-affine (Win32 window/message
	// ownership belongs to the thread that created it); lock this
	// goroutine to its OS thread for the rest of the process's life.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	t, err := tray.New(tray.Callbacks{
		OnOpen: func() { _ = browser.Open(uiServer.URL()) },
		OnQuit: stop,
	})
	if err != nil {
		logger.Warn("tray icon unavailable, continuing headless", "error", err)
		return runner.Run(ctx)
	}

	runner.OnStatusChange(func(st agent.Status) {
		t.SetState(trayStateFor(st))
		t.SetStatusText(trayStatusTextFor(st))
	})
	initial := runner.Status()
	t.SetState(trayStateFor(initial))
	t.SetStatusText(trayStatusTextFor(initial))

	go func() {
		if err := runner.Run(ctx); err != nil {
			logger.Error("agent run loop ended with an error", "error", err)
		}
	}()

	return t.Loop(ctx)
}

// runUpdateLoop checks for a newer version at startup and every
// updateCheckInterval thereafter. When CheckAndApply swaps in and
// relaunches a newer build, this process's own job is done: shutdown (the
// same function OnQuit uses) unwinds the tray loop and the agent run loop
// so this process exits cleanly rather than continuing to run the old
// binary alongside the freshly relaunched one.
func runUpdateLoop(ctx context.Context, updater *update.Updater, shutdown func()) {
	check := func() {
		if updater.CheckAndApply(ctx) {
			shutdown()
		}
	}
	check()

	ticker := time.NewTicker(updateCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

func trayStateFor(st agent.Status) tray.State {
	switch {
	case !st.Paired:
		return tray.StateUnpaired
	case st.Connected:
		return tray.StateConnected
	default:
		return tray.StateDisconnected
	}
}

func trayStatusTextFor(st agent.Status) string {
	switch {
	case !st.Paired:
		return "Sin vincular"
	case st.Connected:
		return "Conectado"
	case st.LastError != "":
		return "Sin conexión (" + st.LastError + ")"
	default:
		return "Sin conexión"
	}
}

// runnerUIAdapter adapts *agent.Runner to ui.Runner. Every method but
// Status is promoted unchanged from the embedded *agent.Runner (they
// already share the exact same internal/spool.PrinterInfo type across
// packages); Status is overridden because ui.Status and agent.Status are
// distinct named types even though their fields match (see ui.Runner's doc
// comment).
type runnerUIAdapter struct {
	*agent.Runner
}

func (a runnerUIAdapter) Status() ui.Status {
	s := a.Runner.Status()
	return ui.Status{
		Paired:        s.Paired,
		Connected:     s.Connected,
		LastError:     s.LastError,
		LastPrintedAt: s.LastPrintedAt,
	}
}
