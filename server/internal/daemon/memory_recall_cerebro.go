package daemon

// CEREBRO-PATCH(daemon-memory-autorecall): FIR-1794 layer 3 — automatic memory
// recall. When the cerebro_memory workspace flag is on, the server recalls
// memories relevant to the task at claim time and ships the rendered block in
// Task.MemoryContext (the memory service is server-side only, so the daemon
// cannot recall itself). This folds it into the agent's start prompt for every
// local runtime type, mirroring how the graphify nudge travels. Empty when
// memory is off or nothing was recalled — the prompt is then unchanged.

import "strings"

// memoryContextBlock returns the automatically recalled memories to inline
// into the start prompt, and true, when the server shipped any. Returns
// "", false when memory is off or nothing was recalled.
func memoryContextBlock(task Task) (string, bool) {
	block := strings.TrimSpace(task.MemoryContext)
	if block == "" {
		return "", false
	}
	return block + "\n\n", true
}
