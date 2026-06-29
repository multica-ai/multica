package agentvault

// SpawnEnv is the per-agent environment the broker may inject at claim time.
//
// FIR-2210 removed the agent-side forward-proxy transport (HTTPS_PROXY relay),
// so the broker now injects nothing — PrepareSpawnEnv returns nil. The type is
// retained as the claim-hook seam that the connections-backed credential path
// (FIR-2166 server-side connection proxy) builds on.
type SpawnEnv map[string]string
