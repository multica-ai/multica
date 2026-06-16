package main

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	original := os.Stdout
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writeEnd
	defer func() {
		os.Stdout = original
	}()

	var buf bytes.Buffer
	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&buf, readEnd)
		copyDone <- copyErr
	}()

	runErr := fn()
	if closeErr := writeEnd.Close(); closeErr != nil && runErr == nil {
		runErr = closeErr
	}
	if copyErr := <-copyDone; copyErr != nil && runErr == nil {
		runErr = copyErr
	}
	if closeErr := readEnd.Close(); closeErr != nil && runErr == nil {
		runErr = closeErr
	}
	return buf.String(), runErr
}
