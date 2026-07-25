package daemon

import "fmt"

// clipToolOutput bounds a tool result to limit bytes while preserving both
// ends of the output.
//
// FIR-3782: the previous behaviour was output[:8192] — head-only. A command
// that prints a lot and then fails (a test suite, a build, a migration) puts
// its error on the last lines, so head-only truncation kept the part that
// succeeded and discarded the diagnosis. Anyone asking "why did this fail?"
// got the preamble and nothing else.
//
// The split is weighted toward the tail because that is where failures land.
func clipToolOutput(output string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(output) <= limit {
		return output
	}

	marker := fmt.Sprintf("\n... [%d bytes truncated] ...\n", len(output)-limit)
	// A limit too small to hold the marker plus context degrades to a plain
	// tail slice — the tail is the part worth keeping.
	if len(marker)+16 > limit {
		return output[len(output)-limit:]
	}

	budget := limit - len(marker)
	head := budget / 3
	tail := budget - head
	return output[:head] + marker + output[len(output)-tail:]
}
