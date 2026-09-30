//go:build windows

package config

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// dpapiProtector encrypts the token with Windows DPAPI at
// CRYPTPROTECT_LOCAL_MACHINE scope, so it can be decrypted by any user
// session on this machine (the agent runs unattended, possibly before any
// user logs on) but not copied to another machine.
type dpapiProtector struct{}

// NewDPAPIProtector returns the production Protector.
func NewDPAPIProtector() Protector { return dpapiProtector{} }

const cryptProtectLocalMachine = 0x4

var (
	modcrypt32  = windows.NewLazySystemDLL("crypt32.dll")
	modkernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procCryptProtectData   = modcrypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = modcrypt32.NewProc("CryptUnprotectData")
	procLocalFree          = modkernel32.NewProc("LocalFree")
)

// dataBlob mirrors the Win32 DATA_BLOB struct used by the CryptProtectData
// family of APIs.
type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newBlob(data []byte) *dataBlob {
	if len(data) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
}

func (b *dataBlob) bytes() []byte {
	if b.cbData == 0 || b.pbData == nil {
		return nil
	}
	out := make([]byte, b.cbData)
	copy(out, unsafe.Slice(b.pbData, int(b.cbData)))
	return out
}

func (dpapiProtector) Protect(plaintext []byte) ([]byte, error) {
	return dpapiCall(procCryptProtectData, plaintext)
}

func (dpapiProtector) Unprotect(ciphertext []byte) ([]byte, error) {
	return dpapiCall(procCryptUnprotectData, ciphertext)
}

// dpapiCall shares the marshaling/unmarshaling logic between protect and
// unprotect: both take one DATA_BLOB in, one DATA_BLOB out, and the same
// trailing flag/out-param signature.
func dpapiCall(proc *windows.LazyProc, input []byte) ([]byte, error) {
	in := newBlob(input)
	var out dataBlob

	r, _, err := proc.Call(
		uintptr(unsafe.Pointer(in)),
		0, // szDataDescr
		0, // pOptionalEntropy
		0, // pvReserved
		0, // pPromptStruct
		uintptr(cryptProtectLocalMachine),
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("config: dpapi call failed: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))

	return out.bytes(), nil
}
