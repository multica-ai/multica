package analytics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRouterWiresAnalyticsProjectorIntoEveryRunWriter(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	assertFileContains(t, filepath.Join(root, "cmd", "server", "router.go"),
		"cerebroanalytics.NewPostgresProjectionSource(pool)",
		"cerebroanalytics.NewPostgresProjectionStore(pool)",
		"h.AnalyticsProjector = analyticsProjector",
		"h.TaskService.AnalyticsProjector = analyticsProjector",
	)
	for _, file := range []string{
		"daemon_cost_bundled_cerebro.go",
		"daemon_cost_context_duplication_cerebro.go",
		"daemon_cost_snapshot_cerebro.go",
		"issue_context_cerebro.go",
		"skill_learning_cerebro.go",
	} {
		assertFileContains(t, filepath.Join(root, "internal", "handler", file), "projectAnalyticsRun(")
	}
}

func assertFileContains(t *testing.T, path string, fragments ...string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range fragments {
		if !strings.Contains(string(body), fragment) {
			t.Errorf("%s missing %q", path, fragment)
		}
	}
}
