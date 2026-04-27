package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var workspaceDuCmd = &cobra.Command{
	Use:   "du",
	Short: "Show disk usage of local workspace directories",
	Long: `Walks the workspaces root and reports the size of each workspace and
the largest task workdirs. Useful observability before a full quota-based GC
policy lands (see #1636).

The workspaces root is resolved as:
  --root flag  >  $MULTICA_WORKSPACES_ROOT  >  ~/multica_workspaces[_<profile>]`,
	RunE: runWorkspaceDu,
}

func init() {
	workspaceCmd.AddCommand(workspaceDuCmd)
	workspaceDuCmd.Flags().String("output", "table", "Output format: table or json")
	workspaceDuCmd.Flags().Int("top", 10, "Show the top N largest task workdirs (0 = all)")
	workspaceDuCmd.Flags().String("root", "", "Override workspaces root (default: profile-derived path)")
}

type taskDu struct {
	WorkspaceID string    `json:"workspace_id"`
	TaskShort   string    `json:"task_short"`
	SizeBytes   int64     `json:"size_bytes"`
	ModTime     time.Time `json:"mod_time"`
	IssueID     string    `json:"issue_id,omitempty"`
	CompletedAt string    `json:"completed_at,omitempty"`
}

type workspaceDu struct {
	WorkspaceID string `json:"workspace_id"`
	Tasks       int    `json:"tasks"`
	SizeBytes   int64  `json:"size_bytes"`
}

type duReport struct {
	Root           string        `json:"root"`
	TotalBytes     int64         `json:"total_bytes"`
	CacheBytes     int64         `json:"cache_bytes"`
	WorkspaceBytes int64         `json:"workspace_bytes"`
	Workspaces     []workspaceDu `json:"workspaces"`
	TopTasks       []taskDu      `json:"top_tasks,omitempty"`
}

func runWorkspaceDu(cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("root")
	if root == "" {
		r, err := resolveWorkspacesRootForCLI(cmd)
		if err != nil {
			return err
		}
		root = r
	}

	report, err := scanWorkspacesDu(root)
	if err != nil {
		return fmt.Errorf("scan %s: %w", root, err)
	}

	// Sort workspaces by size descending for stable output.
	sort.Slice(report.Workspaces, func(i, j int) bool {
		return report.Workspaces[i].SizeBytes > report.Workspaces[j].SizeBytes
	})
	sort.Slice(report.TopTasks, func(i, j int) bool {
		return report.TopTasks[i].SizeBytes > report.TopTasks[j].SizeBytes
	})
	top, _ := cmd.Flags().GetInt("top")
	if top > 0 && len(report.TopTasks) > top {
		report.TopTasks = report.TopTasks[:top]
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, report)
	}
	return printDuTable(os.Stdout, report)
}

// resolveWorkspacesRootForCLI mirrors daemon.LoadConfig's resolution order for
// WorkspacesRoot so `multica workspace du` can run without the daemon:
//
//	$MULTICA_WORKSPACES_ROOT > ~/multica_workspaces[_<profile>]
func resolveWorkspacesRootForCLI(cmd *cobra.Command) (string, error) {
	if v := strings.TrimSpace(os.Getenv("MULTICA_WORKSPACES_ROOT")); v != "" {
		return filepath.Abs(v)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w (pass --root or set MULTICA_WORKSPACES_ROOT)", err)
	}
	profile, _ := cmd.Flags().GetString("profile")
	if profile == "" {
		profile = strings.TrimSpace(os.Getenv("MULTICA_PROFILE"))
	}
	if profile == "" {
		return filepath.Join(home, "multica_workspaces"), nil
	}
	return filepath.Join(home, "multica_workspaces_"+profile), nil
}

func scanWorkspacesDu(root string) (*duReport, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	report := &duReport{Root: root}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(root, e.Name())

		if e.Name() == ".repos" {
			sz, _ := dirSizeBytes(path)
			report.CacheBytes = sz
			report.TotalBytes += sz
			continue
		}

		// Workspace directory: walk its task subdirs.
		ws := workspaceDu{WorkspaceID: e.Name()}
		taskDirs, err := os.ReadDir(path)
		if err != nil {
			continue
		}
		for _, td := range taskDirs {
			if !td.IsDir() {
				continue
			}
			taskPath := filepath.Join(path, td.Name())
			sz, _ := dirSizeBytes(taskPath)
			info, _ := os.Stat(taskPath)
			var modTime time.Time
			if info != nil {
				modTime = info.ModTime()
			}
			task := taskDu{
				WorkspaceID: e.Name(),
				TaskShort:   td.Name(),
				SizeBytes:   sz,
				ModTime:     modTime,
			}
			if b, err := os.ReadFile(filepath.Join(taskPath, ".gc_meta.json")); err == nil {
				var meta struct {
					IssueID     string `json:"issue_id"`
					CompletedAt string `json:"completed_at"`
				}
				if json.Unmarshal(b, &meta) == nil {
					task.IssueID = meta.IssueID
					task.CompletedAt = meta.CompletedAt
				}
			}
			ws.Tasks++
			ws.SizeBytes += sz
			report.TopTasks = append(report.TopTasks, task)
		}
		report.WorkspaceBytes += ws.SizeBytes
		report.TotalBytes += ws.SizeBytes
		report.Workspaces = append(report.Workspaces, ws)
	}
	return report, nil
}

// dirSizeBytes walks path and sums regular-file sizes. Unreadable entries are
// silently skipped so a single permission error doesn't fail the whole report.
func dirSizeBytes(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

func printDuTable(w *os.File, r *duReport) error {
	fmt.Fprintf(w, "Root:             %s\n", r.Root)
	fmt.Fprintf(w, "Workspaces size:  %s\n", humanBytes(r.WorkspaceBytes))
	fmt.Fprintf(w, "Repo cache:       %s (.repos)\n", humanBytes(r.CacheBytes))
	fmt.Fprintf(w, "Total:            %s\n\n", humanBytes(r.TotalBytes))

	if len(r.Workspaces) == 0 {
		fmt.Fprintln(w, "No workspaces found.")
		return nil
	}

	rows := make([][]string, 0, len(r.Workspaces))
	for _, ws := range r.Workspaces {
		rows = append(rows, []string{
			ws.WorkspaceID,
			fmt.Sprintf("%d", ws.Tasks),
			humanBytes(ws.SizeBytes),
		})
	}
	cli.PrintTable(w, []string{"WORKSPACE", "TASKS", "SIZE"}, rows)

	if len(r.TopTasks) == 0 {
		return nil
	}
	fmt.Fprintf(w, "\nTop %d task workdirs by size:\n", len(r.TopTasks))
	taskRows := make([][]string, 0, len(r.TopTasks))
	for _, t := range r.TopTasks {
		issue := t.IssueID
		if len(issue) > 8 {
			issue = issue[:8]
		}
		if issue == "" {
			issue = "(orphan)"
		}
		taskRows = append(taskRows, []string{
			t.TaskShort,
			shortID(t.WorkspaceID),
			humanBytes(t.SizeBytes),
			humanAge(t.ModTime),
			issue,
		})
	}
	cli.PrintTable(w, []string{"TASK", "WORKSPACE", "SIZE", "AGE", "ISSUE"}, taskRows)
	return nil
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func humanAge(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func shortID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
