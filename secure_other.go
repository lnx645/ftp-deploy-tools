//go:build !windows

package main

import (
	"errors"
	"runtime"
)

// ProtectSecret is only available on Windows (DPAPI). On other platforms the
// password must be supplied via the FTPDEPLOY_PASSWORD environment variable.
func ProtectSecret(data []byte) ([]byte, error) {
	return nil, errors.New("enceran DPAPI hanya tersedia di Windows (pakai env FTPDEPLOY_PASSWORD di " + runtime.GOOS + ")")
}

func UnprotectSecret(data []byte) ([]byte, error) {
	return nil, errors.New("enceran DPAPI hanya tersedia di Windows")
}