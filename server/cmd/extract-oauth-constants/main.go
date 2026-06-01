// extract-oauth-constants reads claude's read-only string section and runs
// a set of typed extractors over it to pull out the OAuth constants the
// broker needs at runtime. Used at build time by the broker's pipeline and
// (via the companion claude-version-watcher plan) by CI to keep the embedded
// constants anchored to whatever claude binary we ship.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

type Output struct {
	ExtractedConstants
	Meta Meta `json:"_meta"`
}

type Meta struct {
	ExtractedAt   string `json:"extracted_at"`
	ExtractedFrom string `json:"extracted_from"`
	ClaudeVersion string `json:"claude_version,omitempty"`
	Format        string `json:"format"`
	ExtractorRev  string `json:"extractor_rev"`
}

// Bump on any extraction-semantic change. Lets the watcher diff
// catch unintended behaviour changes when the tool itself is updated.
const extractorRev = "1"

func main() {
	binPath := flag.String("binary", "", "path to claude binary (Mach-O or ELF)")
	outPath := flag.String("out", "", "write JSON to this path (default: stdout)")
	claudeVersion := flag.String("claude-version", "", "claude version string to embed in _meta")
	minLen := flag.Int("min-len", 6, "minimum string length to consider")
	flag.Parse()
	if *binPath == "" {
		fmt.Fprintln(os.Stderr, "usage: extract-oauth-constants -binary <path> [-out <path>] [-claude-version <ver>] [-min-len N]")
		os.Exit(2)
	}

	fmtKind, err := DetectFormat(*binPath)
	if err != nil {
		fatal(err)
	}
	if err := ValidateFormat(*binPath, fmtKind); err != nil {
		fatal(err)
	}

	hits, err := ScanStrings(*binPath, *minLen)
	if err != nil {
		fatal(err)
	}

	consts, errs := Run(hits)
	if len(errs) > 0 {
		fmt.Fprintln(os.Stderr, "extraction failed:")
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  -", e)
		}
		os.Exit(1)
	}

	out := Output{
		ExtractedConstants: consts,
		Meta: Meta{
			ExtractedAt:   time.Now().UTC().Format(time.RFC3339),
			ExtractedFrom: *binPath,
			ClaudeVersion: *claudeVersion,
			Format:        fmtKind.String(),
			ExtractorRev:  extractorRev,
		},
	}

	w := os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fatal(err)
		}
		defer f.Close()
		w = f
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "extract-oauth-constants:", err)
	os.Exit(1)
}
