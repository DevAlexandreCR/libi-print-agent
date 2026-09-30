//go:build windows

package tray

import (
	"context"
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 constants this file needs (winuser.h). Kept local rather than
// pulled from a dependency, matching design.md D8's no-cgo/no-extra-tray-
// dependency fallback.
const (
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmCommand      = 0x0111
	wmLButtonUp    = 0x0202
	wmRButtonUp    = 0x0205
	wmApp          = 0x8000
	wmTrayCallback = wmApp + 1
	wmUpdateIcon   = wmApp + 2

	csHRedraw = 0x0002
	csVRedraw = 0x0001

	wsOverlappedWindow = 0x00CF0000

	mfString    = 0x00000000
	mfGrayed    = 0x00000001
	mfSeparator = 0x00000800

	tpmRightButton = 0x0002
	tpmRightAlign  = 0x0008

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	idMenuStatus = 1001
	idMenuOpen   = 1002
	idMenuQuit   = 1003
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

type msg struct {
	hwnd    windows.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type point struct{ x, y int32 }

// notifyIconDataW mirrors NOTIFYICONDATAW (shellapi.h), the modern (Vista+)
// layout; unused trailing fields still need to be present so cbSize (which
// Explorer uses to determine which fields it may safely touch) matches the
// struct Windows expects for this shell32 version.
type notifyIconDataW struct {
	cbSize           uint32
	hWnd             windows.Handle
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            windows.Handle
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         windows.GUID
	hBalloonIcon     windows.Handle
}

var (
	modShell32 = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassExW   = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW    = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW     = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow      = modUser32.NewProc("DestroyWindow")
	procPostQuitMessage    = modUser32.NewProc("PostQuitMessage")
	procPostMessageW       = modUser32.NewProc("PostMessageW")
	procPostThreadMessageW = modUser32.NewProc("PostThreadMessageW")
	procGetMessageW        = modUser32.NewProc("GetMessageW")
	procTranslateMessage   = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW   = modUser32.NewProc("DispatchMessageW")
	procCreatePopupMenu    = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW        = modUser32.NewProc("AppendMenuW")
	procDestroyMenu        = modUser32.NewProc("DestroyMenu")
	procTrackPopupMenu     = modUser32.NewProc("TrackPopupMenu")
	procSetForegroundWin   = modUser32.NewProc("SetForegroundWindow")
	procGetCursorPos       = modUser32.NewProc("GetCursorPos")

	procShellNotifyIconW = modShell32.NewProc("Shell_NotifyIconW")

	procGetCurrentThreadId = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCurrentThreadId")
)

// Tray owns one tray icon, its hidden message window and its right-click
// menu. Construct with New on the OS thread that will later call Loop
// (Shell_NotifyIcon/window messages are thread-affine); main.go locks that
// thread with runtime.LockOSThread before calling either.
type Tray struct {
	cb Callbacks

	hwnd     windows.Handle
	threadID uint32
	icons    map[State]windows.Handle

	mu         sync.Mutex
	state      State
	statusText string
}

// New registers a hidden top-level window (so TrackPopupMenu/
// SetForegroundWindow behave normally) and adds the tray icon in the
// unpaired state.
func New(cb Callbacks) (*Tray, error) {
	var hInstance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &hInstance); err != nil {
		return nil, fmt.Errorf("tray: GetModuleHandleEx: %w", err)
	}

	className, err := windows.UTF16PtrFromString("LiBiPrintAgentTrayWnd")
	if err != nil {
		return nil, fmt.Errorf("tray: encode class name: %w", err)
	}

	t := &Tray{cb: cb, icons: map[State]windows.Handle{}}

	wc := wndClassExW{
		style:         csHRedraw | csVRedraw,
		lpfnWndProc:   windows.NewCallback(t.wndProc),
		hInstance:     hInstance,
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))

	atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return nil, fmt.Errorf("tray: RegisterClassExW: %w", err)
	}

	windowName, err := windows.UTF16PtrFromString("LiBi Impresión")
	if err != nil {
		return nil, fmt.Errorf("tray: encode window name: %w", err)
	}

	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		uintptr(wsOverlappedWindow),
		0, 0, 0, 0,
		0, 0,
		uintptr(hInstance),
		0,
	)
	if hwnd == 0 {
		return nil, fmt.Errorf("tray: CreateWindowExW: %w", err)
	}
	t.hwnd = windows.Handle(hwnd)

	tid, _, _ := procGetCurrentThreadId.Call()
	t.threadID = uint32(tid)

	for _, s := range []State{StateUnpaired, StateDisconnected, StateConnected} {
		c := stateColors[s]
		icon, err := createSolidIcon(c[0], c[1], c[2])
		if err != nil {
			return nil, err
		}
		t.icons[s] = icon
	}
	t.state = StateUnpaired
	t.statusText = "Sin vincular"

	if err := t.addTrayIcon(); err != nil {
		return nil, err
	}
	return t, nil
}

// Loop runs the Win32 message loop on the calling thread until the user
// chooses "Salir" or ctx is cancelled, then tears down the tray icon,
// menu and window.
func (t *Tray) Loop(ctx context.Context) error {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			procPostThreadMessageW.Call(uintptr(t.threadID), wmClose, 0, 0)
		case <-done:
		}
	}()

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0 = WM_QUIT, -1 = error
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	t.teardown()
	return nil
}

// SetState updates the icon (and, implicitly, the tooltip/menu status
// line the next time it is redrawn) from any goroutine.
func (t *Tray) SetState(s State) {
	t.mu.Lock()
	t.state = s
	t.mu.Unlock()
	if t.hwnd != 0 {
		procPostMessageW.Call(uintptr(t.hwnd), wmUpdateIcon, 0, 0)
	}
}

// SetStatusText updates the disabled menu status line's text. Picked up on
// the next menu open or icon update; no immediate redraw is forced, since
// it always changes together with a SetState call in practice (paired/
// connected transitions), which does trigger one.
func (t *Tray) SetStatusText(text string) {
	t.mu.Lock()
	t.statusText = text
	t.mu.Unlock()
}

func (t *Tray) snapshot() (State, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state, t.statusText
}

func (t *Tray) addTrayIcon() error {
	return t.notify(nimAdd)
}

func (t *Tray) updateTrayIcon() error {
	return t.notify(nimModify)
}

func (t *Tray) removeTrayIcon() {
	_ = t.notify(nimDelete)
}

func (t *Tray) notify(action uint32) error {
	state, text := t.snapshot()
	nid := notifyIconDataW{
		hWnd:             t.hwnd,
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: wmTrayCallback,
		hIcon:            t.icons[state],
	}
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	copyUTF16(nid.szTip[:], "LiBi Impresión - "+text)

	r, _, err := procShellNotifyIconW.Call(uintptr(action), uintptr(unsafe.Pointer(&nid)))
	if r == 0 && action != nimDelete {
		return fmt.Errorf("tray: Shell_NotifyIconW(%d): %w", action, err)
	}
	return nil
}

func copyUTF16(dst []uint16, s string) {
	src := windows.StringToUTF16(s)
	n := len(src)
	if n > len(dst) {
		n = len(dst)
	}
	copy(dst, src[:n])
	if n == len(dst) {
		dst[len(dst)-1] = 0
	}
}

func (t *Tray) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	_, statusText := t.snapshot()
	appendMenuString(menu, mfString|mfGrayed, idMenuStatus, "Estado: "+statusText)
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	appendMenuString(menu, mfString, idMenuOpen, "Abrir LiBi Impresión")
	appendMenuString(menu, mfString, idMenuQuit, "Salir")

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWin.Call(uintptr(t.hwnd))
	procTrackPopupMenu.Call(menu, tpmRightButton|tpmRightAlign, uintptr(pt.x), uintptr(pt.y), 0, uintptr(t.hwnd), 0)
	// Required after TrackPopupMenu so the menu reliably dismisses when the
	// user clicks elsewhere (documented Win32 idiom).
	procPostMessageW.Call(uintptr(t.hwnd), 0, 0, 0)
}

func appendMenuString(menu uintptr, flags, id uint32, text string) {
	ptr, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	procAppendMenuW.Call(menu, uintptr(flags), uintptr(id), uintptr(unsafe.Pointer(ptr)))
}

func (t *Tray) teardown() {
	t.removeTrayIcon()
	for _, icon := range t.icons {
		destroyIcon(icon)
	}
	procDestroyWindow.Call(uintptr(t.hwnd))
}

// wndProc is the window procedure registered for the tray's hidden window.
// It must have exactly this signature to be usable with
// windows.NewCallback (the stdcall WNDPROC ABI).
func (t *Tray) wndProc(hwnd windows.Handle, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmTrayCallback:
		switch uint32(lParam) {
		case wmLButtonUp:
			if t.cb.OnOpen != nil {
				t.cb.OnOpen()
			}
		case wmRButtonUp:
			t.showMenu()
		}
		return 0

	case wmUpdateIcon:
		_ = t.updateTrayIcon()
		return 0

	case wmCommand:
		id := uint32(wParam) & 0xFFFF
		switch id {
		case idMenuOpen:
			if t.cb.OnOpen != nil {
				t.cb.OnOpen()
			}
		case idMenuQuit:
			if t.cb.OnQuit != nil {
				t.cb.OnQuit()
			}
			procPostQuitMessage.Call(0)
		}
		return 0

	case wmClose:
		if t.cb.OnQuit != nil {
			t.cb.OnQuit()
		}
		procPostQuitMessage.Call(0)
		return 0

	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return ret
}
