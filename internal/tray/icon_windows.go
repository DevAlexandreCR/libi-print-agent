//go:build windows

package tray

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// smCxsmicon is GetSystemMetrics' index for the system's small-icon width
// (winuser.h SM_CXSMICON), the size Explorer/the taskbar actually draws a
// Shell_NotifyIcon tray icon at. It already accounts for the user's display
// scaling (16px at 100%, 20 at 125%, 24 at 150%, 32 at 200%, ...), which is
// why loadIcon below queries it instead of hardcoding 16.
const smCxsmicon = 49

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")

	procGetSystemMetrics         = modUser32.NewProc("GetSystemMetrics")
	procCreateIconFromResourceEx = modUser32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon              = modUser32.NewProc("DestroyIcon")
)

// desiredIconSize returns the system's current small-icon size via
// GetSystemMetrics(SM_CXSMICON), falling back to 16 if the call ever
// returns something nonsensical.
func desiredIconSize() int {
	r, _, _ := procGetSystemMetrics.Call(uintptr(smCxsmicon))
	size := int(int32(r))
	if size <= 0 {
		return 16
	}
	return size
}

// loadIcon decodes the embedded .ico for state s and builds an HICON sized
// for desired (see desiredIconSize), via CreateIconFromResourceEx - the
// same API Windows' own icon loader uses once it has picked a directory
// entry (LookupIconIdFromDirectoryEx's usual partner call). Picking the
// entry ourselves (selectIcoImage) rather than calling
// LookupIconIdFromDirectoryEx keeps the whole selection path testable on
// any OS (see icon_assets_test.go), since we fully control the .ico layout
// end to end (tools/genicons writes it, this package reads it).
func loadIcon(s State, desired int) (windows.Handle, error) {
	images, err := parseICO(icoDataFor(s))
	if err != nil {
		return 0, fmt.Errorf("tray: parse embedded icon: %w", err)
	}
	img := selectIcoImage(images, desired)

	h, _, err := procCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&img.Data[0])),
		uintptr(len(img.Data)),
		1,          // fIcon = TRUE (icon, not cursor)
		0x00030000, // dwVer: icon resource format version 3
		uintptr(img.Width),
		uintptr(img.Height),
		0, // flags: LR_DEFAULTCOLOR
	)
	if h == 0 {
		return 0, fmt.Errorf("tray: CreateIconFromResourceEx: %w", err)
	}
	return windows.Handle(h), nil
}

func destroyIcon(h windows.Handle) {
	if h != 0 {
		procDestroyIcon.Call(uintptr(h))
	}
}
