//go:build windows

package main

import (
	"errors"
	"syscall"
	"unsafe"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	crypt32                 = syscall.NewLazyDLL("crypt32.dll")
	procCryptProtectData    = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData  = crypt32.NewProc("CryptUnprotectData")
	procLocalFree           = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newBlob(data []byte) *dataBlob {
	b := &dataBlob{cbData: uint32(len(data))}
	if len(data) > 0 {
		b.pbData = &data[0]
	}
	return b
}

func (b *dataBlob) toBytes() []byte {
	if b == nil || b.pbData == nil || b.cbData == 0 {
		return nil
	}
	return unsafe.Slice(b.pbData, int(b.cbData))
}

// ProtectSecret encrypts a secret with Windows DPAPI bound to the current
// user + machine (same mechanism as Git Credential Manager / Chrome).
func ProtectSecret(data []byte) ([]byte, error) {
	in := newBlob(data)
	var out dataBlob
	r1, _, lastErr := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(in)),
		0, // optional descriptor
		0, // optional entropy
		0, // reserved
		0, // prompt struct
		0, // flags
		uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		return nil, errors.New("CryptProtectData gagal: " + lastErr.Error())
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	blob := out.toBytes()
	if blob == nil {
		return nil, errors.New("CryptProtectData hasil kosong")
	}
	cp := make([]byte, len(blob))
	copy(cp, blob)
	return cp, nil
}

// UnprotectSecret decrypts a secret produced by ProtectSecret on the same
// user + machine.
func UnprotectSecret(data []byte) ([]byte, error) {
	in := newBlob(data)
	var out dataBlob
	r1, _, lastErr := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(in)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		return nil, errors.New("CryptUnprotectData gagal: " + lastErr.Error())
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	blob := out.toBytes()
	if blob == nil {
		return nil, errors.New("CryptUnprotectData hasil kosong")
	}
	cp := make([]byte, len(blob))
	copy(cp, blob)
	return cp, nil
}