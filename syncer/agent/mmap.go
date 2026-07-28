// mmap.go — cross-platform memory-mapped file I/O for agent.
package agent

import (
	"fmt"
	"os"
	"runtime"
)

// MmapFile maps a file into memory read-only.
func MmapFile(f *os.File) ([]byte, error) {
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("mmap stat: %w", err)
	}
	size := fi.Size()
	if size == 0 {
		return []byte{}, nil
	}
	return mmap(f, size)
}

// Munmap unmaps the memory region.
func Munmap(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return munmap(data)
}

// MmapReadOnly opens and mmaps a file by path, returning data + a closer function.
func MmapReadOnly(path string) ([]byte, func() error, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("mmap open %s: %w", path, err)
	}
	data, err := MmapFile(f)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	close := func() error {
		e1 := Munmap(data)
		e2 := f.Close()
		if e1 != nil {
			return e1
		}
		return e2
	}
	runtime.KeepAlive(f)
	return data, close, nil
}
