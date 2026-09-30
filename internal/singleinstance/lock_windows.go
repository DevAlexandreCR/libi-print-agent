//go:build windows

package singleinstance

import (
	"fmt"

	"golang.org/x/sys/windows"
)

type windowsLock struct {
	handle windows.Handle
}

func (l *windowsLock) Release() {
	_ = windows.CloseHandle(l.handle)
}

// acquireOS creates (or opens) the well-known named mutex. CreateMutex
// succeeds either way; ERROR_ALREADY_EXISTS (surfaced as err, not as a
// failed return value) tells us someone else already owns it, matching the
// documented Win32 idiom for this exact pattern.
func acquireOS() (Lock, bool, error) {
	namePtr, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return nil, false, fmt.Errorf("singleinstance: encode mutex name: %w", err)
	}

	handle, err := windows.CreateMutex(nil, false, namePtr)
	if err != nil {
		return nil, false, fmt.Errorf("singleinstance: CreateMutex: %w", err)
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		_ = windows.CloseHandle(handle)
		return nil, false, nil
	}

	return &windowsLock{handle: handle}, true, nil
}
