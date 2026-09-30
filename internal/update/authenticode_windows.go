//go:build windows

package update

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// VerifyAuthenticode checks path's Authenticode signature via WinVerifyTrust
// (wintrust.dll), the same check Windows itself performs (and that
// SmartScreen/AppLocker rely on), so an unsigned or tampered downloaded
// build is never swapped in even if its sha256 happened to match a
// compromised version.txt. UpdateOpts.RequireSignature gates whether this
// is called at all (see updater.go): a dev build with an unsigned running
// binary sets the build-time requireSignature=false flag to skip it.
func VerifyAuthenticode(path string) error {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("update: encode path for authenticode check: %w", err)
	}

	fileInfo := wintrustFileInfo{
		cbStruct:      uint32(unsafe.Sizeof(wintrustFileInfo{})),
		pcwszFilePath: pathPtr,
	}

	trustData := wintrustData{
		cbStruct:            uint32(unsafe.Sizeof(wintrustData{})),
		dwUIChoice:          wtdUINone,
		fdwRevocationChecks: wtdRevokeNone,
		dwUnionChoice:       wtdChoiceFile,
		pFile:               uintptr(unsafe.Pointer(&fileInfo)),
		dwStateAction:       wtdStateActionVerify,
		dwProvFlags:         wtdSaferFlag,
	}
	action := wintrustActionGenericVerifyV2

	ret, _, _ := procWinVerifyTrust.Call(
		uintptr(0), // hwnd: no UI (dwUIChoice = WTD_UI_NONE)
		uintptr(unsafe.Pointer(&action)),
		uintptr(unsafe.Pointer(&trustData)),
	)

	// Always release the per-file state WinVerifyTrust allocated, regardless
	// of the verify outcome; its own return value is not the one that
	// matters here.
	trustData.dwStateAction = wtdStateActionClose
	procWinVerifyTrust.Call(
		uintptr(0),
		uintptr(unsafe.Pointer(&action)),
		uintptr(unsafe.Pointer(&trustData)),
	)

	if int32(ret) != 0 {
		return fmt.Errorf("update: %s failed Authenticode verification (WinVerifyTrust returned 0x%x)", path, uint32(ret))
	}
	return nil
}

var (
	modwintrust        = windows.NewLazySystemDLL("wintrust.dll")
	procWinVerifyTrust = modwintrust.NewProc("WinVerifyTrust")
)

// WINTRUST_ACTION_GENERIC_VERIFY_V2, the standard action GUID for
// "is this file's signature valid" (wintrust.h).
var wintrustActionGenericVerifyV2 = windows.GUID{
	Data1: 0xaac56b,
	Data2: 0xcd44,
	Data3: 0x11d0,
	Data4: [8]byte{0x8c, 0xc2, 0x00, 0xc0, 0x4f, 0xc2, 0x95, 0xee},
}

const (
	wtdUINone            = 2
	wtdRevokeNone        = 0
	wtdChoiceFile        = 1
	wtdStateActionVerify = 1
	wtdStateActionClose  = 2
	wtdSaferFlag         = 0x100
)

// wintrustFileInfo mirrors WINTRUST_FILE_INFO (wintrust.h): describes the
// single file being verified.
type wintrustFileInfo struct {
	cbStruct       uint32
	pcwszFilePath  *uint16
	hFile          windows.Handle
	pgKnownSubject *windows.GUID
}

// wintrustData mirrors WINTRUST_DATA (wintrust.h), the older (pre-
// pSignatureSettings) layout, which WinVerifyTrust accepts based on
// cbStruct. pFile is the WTD_CHOICE_FILE arm of the union as a raw pointer.
type wintrustData struct {
	cbStruct            uint32
	pPolicyCallbackData uintptr
	pSIPClientData      uintptr
	dwUIChoice          uint32
	fdwRevocationChecks uint32
	dwUnionChoice       uint32
	pFile               uintptr
	dwStateAction       uint32
	hWVTStateData       windows.Handle
	pwszURLReference    *uint16
	dwProvFlags         uint32
	dwUIContext         uint32
}
