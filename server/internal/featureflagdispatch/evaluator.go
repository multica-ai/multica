package featureflagdispatch

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const defaultSnapshotVersion uint64 = 1

// Evaluator renders the current server-side decisions for daemon-bound flags.
type Evaluator struct {
	flags   *featureflag.Service
	version uint64
}

// NewEvaluator returns an evaluator for the currently loaded server feature
// flag configuration. The first implementation has no live reload path, so
// version starts at 1 for the process lifetime.
func NewEvaluator(flags *featureflag.Service) *Evaluator {
	return &Evaluator{flags: flags, version: defaultSnapshotVersion}
}

// EvaluateForRuntime returns the complete daemon-bound flag snapshot for rt.
// Missing server config is still an explicit "off" decision for registered
// daemon-bound flags; the daemon's local StaticProvider is only a fallback when
// it is talking to an old server that sends no snapshot field at all.
func (e *Evaluator) EvaluateForRuntime(ctx context.Context, rt db.AgentRuntime) *protocol.DaemonFeatureFlagSnapshot {
	if e == nil {
		return nil
	}
	version := e.version
	if version == 0 {
		version = defaultSnapshotVersion
	}
	flags := make(map[string]string, len(DaemonBoundFlags))
	evalCtx := featureflag.EvalContext{Attributes: map[string]string{}}
	if rt.DaemonID.Valid {
		evalCtx.Attributes["daemon_id"] = rt.DaemonID.String
	}
	ctx = featureflag.WithEvalContext(ctx, evalCtx)
	for _, key := range DaemonBoundFlags {
		flags[key] = e.flags.Variant(ctx, key, "off")
	}
	return &protocol.DaemonFeatureFlagSnapshot{
		Version: version,
		Flags:   flags,
	}
}
