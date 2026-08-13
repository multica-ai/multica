// Package pluginruntime contains the versioned, JSON-serializable data that
// connects a compiled plugin snapshot to a task execution manifest.
package pluginruntime

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

const (
	CompilerVersion = "plugin-compiler-v1"
	ComposerVersion = "plugin-composer-v1"
	SchemaVersion   = 1
)

// CompiledEntry is one contribution made available at a workspace or agent
// scope. Execution manifests copy these entries verbatim at enqueue time.
type CompiledEntry struct {
	PluginID               string   `json:"plugin_id"`
	PluginKey              string   `json:"plugin_key"`
	InstallationID         string   `json:"installation_id"`
	ReleaseID              string   `json:"release_id"`
	ReleaseVersion         string   `json:"release_version"`
	ContributionID         string   `json:"contribution_id"`
	ContributionKey        string   `json:"contribution_key"`
	ContributionType       string   `json:"contribution_type"`
	DisplayName            string   `json:"display_name"`
	Description            string   `json:"description,omitempty"`
	SourceKind             string   `json:"source_kind"`
	ArtifactFileID         string   `json:"artifact_file_id"`
	ArtifactRef            string   `json:"artifact_ref"`
	ArtifactDigest         string   `json:"artifact_digest"`
	EntryPath              string   `json:"entry_path"`
	EntryDigest            string   `json:"entry_digest"`
	RequiredDaemonFeatures []string `json:"required_daemon_features"`
	Ordinal                int32    `json:"ordinal"`
	ScopeType              string   `json:"scope_type"`
	ScopeID                string   `json:"scope_id"`
	ExcludedAgentIDs       []string `json:"excluded_agent_ids,omitempty"`
	SkillBundleHash        string   `json:"skill_bundle_hash"`
	SkillSizeBytes         int64    `json:"skill_size_bytes"`
	SkillFileCount         int      `json:"skill_file_count"`
}

type SnapshotPayload struct {
	SourceGenerations map[string]int64 `json:"source_generations"`
	CompilerVersion   string           `json:"compiler_version"`
	SchemaVersion     int              `json:"schema_version"`
	Entries           []CompiledEntry  `json:"entries"`
}

func Canonicalize(entries []CompiledEntry) {
	for i := range entries {
		sort.Strings(entries[i].ExcludedAgentIDs)
		sort.Strings(entries[i].RequiredDaemonFeatures)
	}
	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		if left.Ordinal != right.Ordinal {
			return left.Ordinal < right.Ordinal
		}
		if left.PluginKey != right.PluginKey {
			return left.PluginKey < right.PluginKey
		}
		if left.ContributionKey != right.ContributionKey {
			return left.ContributionKey < right.ContributionKey
		}
		if left.ScopeType != right.ScopeType {
			return left.ScopeType < right.ScopeType
		}
		return left.ScopeID < right.ScopeID
	})
}

func SnapshotDigest(sourceGenerations map[string]int64, entries []CompiledEntry) (string, error) {
	Canonicalize(entries)
	content, err := json.Marshal(SnapshotPayload{
		SourceGenerations: sourceGenerations,
		CompilerVersion:   CompilerVersion,
		SchemaVersion:     SchemaVersion,
		Entries:           entries,
	})
	if err != nil {
		return "", fmt.Errorf("marshal plugin snapshot payload: %w", err)
	}
	return plugincontract.DigestBytes(content), nil
}

func ParseEntries(raw []byte) ([]CompiledEntry, error) {
	var entries []CompiledEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decode plugin compiled entries: %w", err)
	}
	return entries, nil
}
