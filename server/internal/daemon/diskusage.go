package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// TaskDiskUsage describes one task workdir's footprint on disk.
type TaskDiskUsage struct {
	WorkspaceID       string `json:"workspace_id"`
	WorkspaceShort    string `json:"workspace_short"`
	TaskShort         string `json:"task_short"`
	Path              string `json:"path"`
	Kind              string `json:"kind"`
	ParentStatus      string `json:"parent_status"`
	AgeSeconds        int64  `json:"age_seconds"`
	SizeBytes         int64  `json:"size_bytes"`
	ArtifactSizeBytes int64  `json:"artifact_size_bytes"`
}

// WorkspaceDiskUsage aggregates per-workspace footprint across all tasks.
// ArtifactRatio is the fraction (0..1) of SizeBytes that the GC artifact
// cleanup could reclaim — kept here so the JSON consumer doesn't have to
// re-derive it (and so the table view can render the column without dividing
// by zero on empty workspaces).
type WorkspaceDiskUsage struct {
	WorkspaceID       string  `json:"workspace_id"`
	WorkspaceShort    string  `json:"workspace_short"`
	TaskCount         int     `json:"task_count"`
	SizeBytes         int64   `json:"size_bytes"`
	ArtifactSizeBytes int64   `json:"artifact_size_bytes"`
	ArtifactRatio     float64 `json:"artifact_ratio"`
	OldestAgeSeconds  int64   `json:"oldest_age_seconds"`
}

// DiskUsageReport is the full result of a single ScanDiskUsage call. Total*
// fields always reflect the entire scan, never the post-`--top` truncated
// view — consumers that need the displayed subtotals can sum the slice.
type DiskUsageReport struct {
	WorkspacesRoot         string               `json:"workspaces_root"`
	GeneratedAt            time.Time            `json:"generated_at"`
	ArtifactPatterns       []string             `json:"artifact_patterns"`
	Tasks                  []TaskDiskUsage      `json:"tasks"`
	Workspaces             []WorkspaceDiskUsage `json:"workspaces"`
	TotalTaskCount         int                  `json:"total_task_count"`
	TotalWorkspaceCount    int                  `json:"total_workspace_count"`
	TotalSizeBytes         int64                `json:"total_size_bytes"`
	TotalArtifactSizeBytes int64                `json:"total_artifact_size_bytes"`
	TotalArtifactRatio     float64              `json:"total_artifact_ratio"`
}

// DiskUsageKindUnknown is the kind reported for task directories whose
// .gc_meta.json is missing or unreadable. Mirrors how the GC orphan path
// treats them — present on disk, but no parent record we can lock onto.
const DiskUsageKindUnknown = "unknown"

// ScanDiskUsage walks workspacesRoot and returns the disk-usage report. The
// walk is read-only and follows the same safety contract as the GC artifact
// cleaner: it never enters .git, never follows symlinks, and counts only
// regular files. artifactPatterns is filtered through the basename-only check
// used by cleanTaskArtifacts so the reported "artifact" footprint matches the
// bytes the GC would actually reclaim. Missing roots return an empty report
// (not an error) — a daemon that's never run yet has no directory to walk.
func ScanDiskUsage(workspacesRoot string, artifactPatterns []string) (DiskUsageReport, error) {
	report := DiskUsageReport{
		WorkspacesRoot:   workspacesRoot,
		GeneratedAt:      time.Now().UTC(),
		ArtifactPatterns: nil,
	}
	if workspacesRoot == "" {
		return report, fmt.Errorf("disk-usage: workspaces root is required")
	}

	patternSet := buildPatternSet(artifactPatterns)
	report.ArtifactPatterns = sortedKeys(patternSet)

	wsEntries, err := os.ReadDir(workspacesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return report, nil
		}
		return report, fmt.Errorf("disk-usage: read workspaces root: %w", err)
	}

	wsAgg := map[string]*WorkspaceDiskUsage{}

	for _, wsEntry := range wsEntries {
		// Skip the bare-repo cache and any non-directory entries; the GC loop
		// applies the same exclusions, so the disk-usage report stays in sync
		// with what the GC actually walks.
		if !wsEntry.IsDir() || wsEntry.Name() == ".repos" {
			continue
		}
		wsID := wsEntry.Name()
		wsDir := filepath.Join(workspacesRoot, wsID)
		taskEntries, err := os.ReadDir(wsDir)
		if err != nil {
			continue
		}
		for _, t := range taskEntries {
			if !t.IsDir() {
				continue
			}
			taskDir := filepath.Join(wsDir, t.Name())
			usage := buildTaskUsage(taskDir, wsID, t.Name(), patternSet)

			report.Tasks = append(report.Tasks, usage)
			report.TotalSizeBytes += usage.SizeBytes
			report.TotalArtifactSizeBytes += usage.ArtifactSizeBytes

			ws, ok := wsAgg[wsID]
			if !ok {
				ws = &WorkspaceDiskUsage{
					WorkspaceID:    wsID,
					WorkspaceShort: ShortID(wsID),
				}
				wsAgg[wsID] = ws
			}
			ws.TaskCount++
			ws.SizeBytes += usage.SizeBytes
			ws.ArtifactSizeBytes += usage.ArtifactSizeBytes
			if usage.AgeSeconds > ws.OldestAgeSeconds {
				ws.OldestAgeSeconds = usage.AgeSeconds
			}
		}
	}

	sort.Slice(report.Tasks, func(i, j int) bool {
		return report.Tasks[i].SizeBytes > report.Tasks[j].SizeBytes
	})

	report.Workspaces = make([]WorkspaceDiskUsage, 0, len(wsAgg))
	for _, ws := range wsAgg {
		ws.ArtifactRatio = ratio(ws.ArtifactSizeBytes, ws.SizeBytes)
		report.Workspaces = append(report.Workspaces, *ws)
	}
	sort.Slice(report.Workspaces, func(i, j int) bool {
		return report.Workspaces[i].SizeBytes > report.Workspaces[j].SizeBytes
	})

	report.TotalTaskCount = len(report.Tasks)
	report.TotalWorkspaceCount = len(report.Workspaces)
	report.TotalArtifactRatio = ratio(report.TotalArtifactSizeBytes, report.TotalSizeBytes)

	return report, nil
}

// ratio returns numerator / denominator, mapping 0/0 (and any 0 denominator)
// to 0 instead of NaN. Callers render the result as a percentage so a NaN
// would surface as "NaN%" in the table — guard at the source.
func ratio(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func buildPatternSet(patterns []string) map[string]struct{} {
	set := make(map[string]struct{}, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" || strings.ContainsAny(p, "/\\") {
			continue
		}
		set[p] = struct{}{}
	}
	return set
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func buildTaskUsage(taskDir, wsID, taskShort string, patternSet map[string]struct{}) TaskDiskUsage {
	usage := TaskDiskUsage{
		WorkspaceID:    wsID,
		WorkspaceShort: ShortID(wsID),
		TaskShort:      taskShort,
		Path:           taskDir,
		Kind:           DiskUsageKindUnknown,
	}

	if meta, err := execenv.ReadGCMeta(taskDir); err == nil && meta != nil {
		usage.Kind = string(meta.Kind)
		if !meta.CompletedAt.IsZero() {
			usage.AgeSeconds = int64(time.Since(meta.CompletedAt).Seconds())
		}
	}
	// Fall back to mtime when meta is missing or didn't carry a completed_at.
	// Matches the orphanByMTime path the GC loop takes for the same case.
	if usage.AgeSeconds <= 0 {
		if info, err := os.Stat(taskDir); err == nil {
			usage.AgeSeconds = int64(time.Since(info.ModTime()).Seconds())
		}
	}

	usage.SizeBytes, usage.ArtifactSizeBytes = taskSize(taskDir, patternSet)
	return usage
}

// taskSize walks taskDir and returns (totalBytes, artifactBytes). Both honor
// the GC safety contract: never descends into .git, never follows symlinks,
// counts only regular files. A directory whose basename matches patternSet
// is treated as an artifact subtree — its size is added to both totals and
// the walk does not descend further so the size matches what os.RemoveAll
// would reclaim if the GC ran cleanTaskArtifacts on it.
func taskSize(taskDir string, patternSet map[string]struct{}) (totalBytes int64, artifactBytes int64) {
	if taskDir == "" {
		return
	}
	absRoot, err := filepath.Abs(taskDir)
	if err != nil {
		return
	}

	_ = filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == absRoot {
			return nil
		}
		// Symlinks: never followed, never counted. WalkDir already refuses to
		// descend through them, but a symlinked file would otherwise show up
		// here as a non-dir entry — drop it explicitly so the size stays
		// consistent with cleanTaskArtifacts' refusal to touch link targets.
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			if _, ok := patternSet[entry.Name()]; ok {
				rel, relErr := filepath.Rel(absRoot, path)
				if relErr != nil || rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
					return filepath.SkipDir
				}
				size := dirSize(path)
				totalBytes += size
				artifactBytes += size
				return filepath.SkipDir
			}
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			totalBytes += info.Size()
		}
		return nil
	})
	return
}

// ShortID returns the first 8 chars (dashes stripped) of a UUID, falling back
// to the raw input when shorter. Mirrors execenv.shortID, which lives in an
// internal subpackage and isn't exported.
func ShortID(id string) string {
	s := strings.ReplaceAll(id, "-", "")
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
