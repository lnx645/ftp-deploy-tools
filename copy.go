package main

import "io"

func copyBuffer(dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 1024*1024)
	return io.CopyBuffer(dst, src, buf)
}