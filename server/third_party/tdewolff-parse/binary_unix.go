//go:build unix

package parse

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

type binaryReaderMmap struct {
	data []byte
	size int64
}

func newBinaryReaderMmap(f *os.File) (*binaryReaderMmap, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("mmap: not a regular file: %v", info.Name())
	}

	size := info.Size()
	if size == 0 {
		// Treat (size == 0) as a special case, avoiding the syscall, since
		// "man 2 mmap" says "the length... must be greater than 0".
		//
		// As we do not call syscall.Mmap, there is no need to call
		// runtime.SetFinalizer to enforce a balancing syscall.Munmap.
		return &binaryReaderMmap{
			data: make([]byte, 0),
		}, nil
	} else if size < 0 {
		return nil, fmt.Errorf("mmap: file %s has negative size", info.Name())
	} else if size != int64(int(size)) {
		return nil, fmt.Errorf("mmap: file %s is too large", info.Name())
	}

	data, err := syscall.Mmap(int(f.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, err
	}
	r := &binaryReaderMmap{data, size}
	//runtime.SetFinalizer(r, (*binaryReaderMmap).Close)
	return r, nil
}

// Close closes the reader.
func (r *binaryReaderMmap) Close() error {
	if r.data == nil {
		return nil
	} else if len(r.data) == 0 {
		r.data = nil
		return nil
	}
	data := r.data
	r.data = nil
	//runtime.SetFinalizer(r, nil)
	return syscall.Munmap(data)
}

// Len returns the length of the underlying memory-mapped file.
func (r *binaryReaderMmap) Len() int64 {
	return r.size
}

func (r *binaryReaderMmap) Bytes(b []byte, n, off int64) ([]byte, error) {
	var err error
	if r.data == nil {
		return nil, errors.New("mmap: closed")
	} else if off < 0 || n < 0 {
		return nil, fmt.Errorf("mmap: invalid range %d--%d", off, off+n)
	} else if int64(len(r.data)) <= off {
		return nil, io.EOF
	} else if int64(len(r.data))-off <= n {
		n = int64(len(r.data)) - off
		err = io.EOF
	}

	data := r.data[off : off+n : off+n]
	if b == nil {
		return data, err
	}
	copy(b, data)
	return b[:len(data)], err
}

func NewBinaryReaderMmapPath(filename string) (*BinaryReader, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	} else if mmap, err := newBinaryReaderMmap(f); err != nil {
		f.Close()
		return nil, err
	} else if err := f.Close(); err != nil {
		mmap.Close()
		return nil, err
	} else {
		return NewBinaryReader(mmap), nil
	}
}

func NewBinaryReaderMmapFile(f *os.File) (*BinaryReader, error) {
	file, err := newBinaryReaderMmap(f)
	if err != nil {
		return nil, err
	}
	return NewBinaryReader(file), nil
}
