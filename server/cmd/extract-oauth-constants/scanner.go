package main

import (
	"debug/elf"
	"debug/macho"
	"fmt"
	"io"
	"os"
)

// StringHit is a single null-terminated printable run from a binary.
// Offset is the byte offset within the file. File-relative offsets work
// uniformly for any executable layout — including Bun-compiled binaries
// where the strings of interest live inside a bundled-JS segment (__BUN
// on Mach-O), not in __cstring.
type StringHit struct {
	Offset int64
	Value  string
}

type Format int

const (
	FormatUnknown Format = iota
	FormatMachO
	FormatELF
)

func (f Format) String() string {
	switch f {
	case FormatMachO:
		return "mach-o"
	case FormatELF:
		return "elf"
	default:
		return "unknown"
	}
}

// DetectFormat reads the first 4 magic bytes and decides Mach-O vs ELF.
// Used only to populate the _meta.format field in the output; extraction
// itself is format-agnostic (whole-file string scan).
func DetectFormat(path string) (Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return FormatUnknown, err
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return FormatUnknown, err
	}
	switch {
	case magic == [4]byte{0x7f, 'E', 'L', 'F'}:
		return FormatELF, nil
	case magic[0] == 0xfe && magic[1] == 0xed && magic[2] == 0xfa && (magic[3] == 0xce || magic[3] == 0xcf),
		magic[3] == 0xfe && magic[2] == 0xed && magic[1] == 0xfa && (magic[0] == 0xce || magic[0] == 0xcf),
		magic[0] == 0xca && magic[1] == 0xfe && magic[2] == 0xba && magic[3] == 0xbe:
		return FormatMachO, nil
	}
	return FormatUnknown, fmt.Errorf("unrecognised magic bytes: % x", magic)
}

// ValidateFormat opens the file with the appropriate parser to confirm it's
// a well-formed executable. Doesn't extract anything — that happens in
// ScanStrings, which works directly on the raw bytes.
func ValidateFormat(path string, fmtKind Format) error {
	switch fmtKind {
	case FormatMachO:
		f, err := macho.Open(path)
		if err != nil {
			return fmt.Errorf("not a valid mach-o: %w", err)
		}
		return f.Close()
	case FormatELF:
		f, err := elf.Open(path)
		if err != nil {
			return fmt.Errorf("not a valid elf: %w", err)
		}
		return f.Close()
	}
	return fmt.Errorf("unsupported format: %s", fmtKind)
}

// ScanStrings reads the whole binary and returns every printable-ASCII run
// of length >= minLen. Offsets are file-relative.
//
// Whole-file scanning is deliberate: Bun-compiled binaries (like Claude Code)
// embed the entire JS bundle (where every OAuth constant actually lives) in
// a custom segment (__BUN on Mach-O). Section-walking ELF/Mach-O parsers
// see __cstring containing irrelevant C runtime strings and miss the JS
// payload entirely. Whole-file scanning is invariant to the bundler.
func ScanStrings(path string, minLen int) ([]StringHit, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return extractCStrings(data, minLen), nil
}

// extractCStrings walks data and returns every printable-ASCII run of
// length >= minLen, with offset relative to the start of data.
func extractCStrings(data []byte, minLen int) []StringHit {
	var out []StringHit
	start := -1
	for i, b := range data {
		printable := b >= 0x20 && b < 0x7f
		switch {
		case printable && start < 0:
			start = i
		case !printable && start >= 0:
			if i-start >= minLen {
				out = append(out, StringHit{Offset: int64(start), Value: string(data[start:i])})
			}
			start = -1
		}
	}
	if start >= 0 && len(data)-start >= minLen {
		out = append(out, StringHit{Offset: int64(start), Value: string(data[start:])})
	}
	return out
}
