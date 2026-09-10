package main

import (
	"os"
	"runtime"
	"syscall"
)

// isReparsePoint reports whether path is a symlink or directory junction
// (Windows reparse point). Broken junctions otherwise cause "Incorrect function"
// read errors when walked.
func isReparsePoint(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return true
	}
	if runtime.GOOS == "windows" {
		if sys, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
			return sys.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
		}
	}
	return false
}