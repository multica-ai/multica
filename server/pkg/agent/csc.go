package agent

// cscBackend implements Backend by spawning the CSC CLI, which speaks the
// same stream-json protocol as Claude Code.
type cscBackend struct{ streamJSONBackend }
