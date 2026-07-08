package execenv

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"

	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ServerSnapshot is the daemon-local copy of the server-evaluated
// daemon-bound feature flag decisions.
type ServerSnapshot struct {
	Version uint64
	Flags   map[string]string
}

// ServerSnapshotProvider serves decisions delivered by the server over daemon
// heartbeat acks. Apply swaps the entire snapshot atomically.
type ServerSnapshotProvider struct {
	snap atomic.Pointer[ServerSnapshot]
}

var activeServerSnapshotProvider atomic.Pointer[ServerSnapshotProvider]

func NewServerSnapshotProvider() *ServerSnapshotProvider {
	return &ServerSnapshotProvider{}
}

// Name implements featureflag.Provider.
func (*ServerSnapshotProvider) Name() string { return "server_snapshot" }

// Lookup implements featureflag.Provider.
func (p *ServerSnapshotProvider) Lookup(_ context.Context, key string) (featureflag.Decision, bool) {
	if p == nil {
		return featureflag.Decision{}, false
	}
	snap := p.snap.Load()
	if snap == nil {
		return featureflag.Decision{}, false
	}
	variant, ok := snap.Flags[key]
	if !ok {
		return featureflag.Decision{}, false
	}
	return featureflag.Decision{
		Key:     key,
		Enabled: snapshotVariantEnabled(variant),
		Variant: variant,
		Reason:  featureflag.ReasonStatic,
		Source:  p.Name(),
	}, true
}

// Apply atomically replaces the provider's current snapshot.
func (p *ServerSnapshotProvider) Apply(snapshot ServerSnapshot) {
	if p == nil {
		return
	}
	clone := make(map[string]string, len(snapshot.Flags))
	for key, variant := range snapshot.Flags {
		clone[key] = variant
	}
	p.snap.Store(&ServerSnapshot{
		Version: snapshot.Version,
		Flags:   clone,
	})
}

// Clear drops the current server snapshot so lookups fall through to the next
// provider. This is how a new daemon behaves when an old server omits the
// feature_flags heartbeat field.
func (p *ServerSnapshotProvider) Clear() {
	if p == nil {
		return
	}
	p.snap.Store(nil)
}

// Snapshot returns a copy of the current snapshot for tests and diagnostics.
func (p *ServerSnapshotProvider) Snapshot() (ServerSnapshot, bool) {
	if p == nil {
		return ServerSnapshot{}, false
	}
	snap := p.snap.Load()
	if snap == nil {
		return ServerSnapshot{}, false
	}
	clone := make(map[string]string, len(snap.Flags))
	for key, variant := range snap.Flags {
		clone[key] = variant
	}
	return ServerSnapshot{Version: snap.Version, Flags: clone}, true
}

// SetServerSnapshotProvider installs the provider that heartbeat acks update.
// Passing nil disables heartbeat-driven updates.
func SetServerSnapshotProvider(p *ServerSnapshotProvider) {
	activeServerSnapshotProvider.Store(p)
}

// ApplyFeatureFlagSnapshot applies a protocol snapshot from a heartbeat ack to
// the installed ServerSnapshotProvider. Old servers omit the field, so nil
// clears any prior server snapshot and lets local env/YAML fallback win.
func ApplyFeatureFlagSnapshot(snapshot *protocol.DaemonFeatureFlagSnapshot) {
	p := activeServerSnapshotProvider.Load()
	if p == nil {
		return
	}
	if snapshot == nil {
		p.Clear()
		return
	}
	p.Apply(ServerSnapshot{
		Version: snapshot.Version,
		Flags:   snapshot.Flags,
	})
}

// NewDaemonFeatureFlagServiceFromEnv builds the daemon-side provider chain:
//
//   - EnvProvider: local FF_* emergency overrides.
//   - ServerSnapshotProvider: decisions delivered over heartbeat.
//   - StaticProvider: local YAML fallback for old servers / self-hosted rescue.
//   - Caller default.
func NewDaemonFeatureFlagServiceFromEnv(logger *slog.Logger) (*ServerSnapshotProvider, *featureflag.Service, error) {
	serverSnapshot := NewServerSnapshotProvider()
	providers := []featureflag.Provider{
		featureflag.NewEnvProvider(featureflag.EnvOverridePrefix),
		serverSnapshot,
	}

	path := strings.TrimSpace(os.Getenv(featureflag.EnvFlagFile))
	var loadedCount int
	if path != "" {
		rules, err := featureflag.LoadRulesFromYAMLFile(path)
		if err != nil {
			return nil, nil, err
		}
		static := featureflag.NewStaticProvider()
		static.LoadRules(rules)
		providers = append(providers, static)
		loadedCount = len(rules)
	}

	var opts []featureflag.Option
	if logger != nil {
		opts = append(opts, featureflag.WithLogger(logger))
	}
	svc := featureflag.NewService(featureflag.NewChainProvider(providers...), opts...)
	if logger != nil {
		logger.Info("daemon feature flags initialised",
			slog.String("file", path),
			slog.Int("rules", loadedCount),
			slog.String("env_prefix", featureflag.EnvOverridePrefix),
			slog.String("provider_order", "env,server_snapshot,static"),
		)
	}
	return serverSnapshot, svc, nil
}

func snapshotVariantEnabled(v string) bool {
	switch v {
	case "", "off", "false", "0":
		return false
	default:
		return true
	}
}
