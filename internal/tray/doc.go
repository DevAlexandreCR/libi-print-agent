// Package tray implements the Windows notification-area ("system tray")
// icon (task 8.4): connected/disconnected/unpaired icon states, a right-
// click menu ("Abrir LiBi Impresión", a disabled status line, "Salir"),
// and opening the status page on a left click. It is a minimal, dependency-
// free Shell_NotifyIcon binding over golang.org/x/sys/windows (no cgo, so
// it still cross-compiles from this repo's Linux/macOS build containers),
// per design.md D8's explicit fallback: "if none [pure-Go tray lib] builds
// without cgo for windows, implement minimal Shell_NotifyIcon via
// x/sys/windows". Icons are generated at runtime (CreateIcon over a solid
// color) rather than embedded .ico assets.
package tray

// State is the tray icon's visual state.
type State int

const (
	StateUnpaired State = iota
	StateDisconnected
	StateConnected
)

// Callbacks receives the menu/click actions task 8.4 wires into main: OnOpen
// opens the status page (tray icon click, or "Abrir LiBi Impresión"),
// OnQuit is the user choosing "Salir" (the caller is responsible for
// actually shutting the process down; Loop returns right after calling it).
type Callbacks struct {
	OnOpen func()
	OnQuit func()
}
