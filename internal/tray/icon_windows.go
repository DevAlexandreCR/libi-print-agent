//go:build windows

package tray

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const iconSize = 16

var (
	modUser32       = windows.NewLazySystemDLL("user32.dll")
	procCreateIcon  = modUser32.NewProc("CreateIcon")
	procDestroyIcon = modUser32.NewProc("DestroyIcon")
)

// createSolidIcon builds a small opaque square icon of the given color via
// CreateIcon (no .ico asset needed - see package doc). AND mask all-zero +
// a 32bpp XOR bitmap with alpha=0xFF is the standard way to get a plain
// opaque color icon out of CreateIcon without a bitmap resource.
func createSolidIcon(r, g, b byte) (windows.Handle, error) {
	andMaskSize := ((iconSize + 7) / 8) * iconSize
	andMask := make([]byte, andMaskSize)

	xorMask := make([]byte, iconSize*iconSize*4)
	for i := 0; i < iconSize*iconSize; i++ {
		xorMask[i*4+0] = b
		xorMask[i*4+1] = g
		xorMask[i*4+2] = r
		xorMask[i*4+3] = 0xFF
	}

	h, _, err := procCreateIcon.Call(
		0, // hInstance: unused by CreateIcon beyond bookkeeping
		uintptr(iconSize),
		uintptr(iconSize),
		1,  // cPlanes
		32, // cBitsPixel
		uintptr(unsafe.Pointer(&andMask[0])),
		uintptr(unsafe.Pointer(&xorMask[0])),
	)
	if h == 0 {
		return 0, fmt.Errorf("tray: CreateIcon: %w", err)
	}
	return windows.Handle(h), nil
}

func destroyIcon(h windows.Handle) {
	if h != 0 {
		procDestroyIcon.Call(uintptr(h))
	}
}

// stateColors maps each State to its icon's RGB color: green for
// connected, red/orange for disconnected (paired but not reachable), gray
// for unpaired.
var stateColors = map[State][3]byte{
	StateConnected:    {0x16, 0xa3, 0x4a}, // green
	StateDisconnected: {0xdc, 0x26, 0x26}, // red
	StateUnpaired:     {0x94, 0xa3, 0xb8}, // gray
}
