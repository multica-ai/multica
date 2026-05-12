package main

import (
	"encoding/json"
	"net/http"
	"runtime"
)

// buildInfoHandler returns the compile-time-baked metadata about the
// running binary. The deploy workflow uses this to verify that the
// container it just brought up is actually serving the SHA that was
// just built — closes the "deploy succeeded but container serves
// stale code" failure mode from 2026-05-12 where env.current_sha
// showed e72c66dd but the host was running ea58198.
//
// Unauthenticated endpoint at /build_info. Returns:
//
//	{
//	  "commit":   "<git-sha>",   // injected via -ldflags -X main.commit
//	  "version":  "<version>",   // injected via -ldflags -X main.version
//	  "go_version": "go1.x.y"    // for diagnostic context
//	}
//
// When the binary was built without ldflags injection (e.g. local
// `go build` without the Makefile/Dockerfile flags), commit defaults
// to "unknown" and version to "dev" — see the package-level vars in
// main.go. The deploy workflow's verifier treats "unknown" as a
// hard failure: a dev-build container should never make it to prod.
func buildInfoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		payload := struct {
			Commit    string `json:"commit"`
			Version   string `json:"version"`
			GoVersion string `json:"go_version"`
		}{
			Commit:    commit,
			Version:   version,
			GoVersion: runtime.Version(),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}
}
