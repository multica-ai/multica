package handler

import (
	"context"
	"fmt"
)

// missingOCRRunner simulates a host without OCR binaries so the attachment
// fallback test is deterministic on every development and CI machine.
type missingOCRRunner struct{}

func (missingOCRRunner) Run(_ context.Context, _, name string, _ ...string) ([]byte, error) {
	return nil, fmt.Errorf("%s: executable file not found in $PATH", name)
}
