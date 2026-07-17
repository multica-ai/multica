package aiimpact

import (
	"os"
	"strings"
	"testing"
)

func TestMigrationIsWorkspaceScopedAppendOnlyAndHasNoIllustrativeData(t *testing.T) {
	up, err := os.ReadFile("../../../migrations/9145_cerebro_ai_impact.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(up)
	for _, required := range []string{"cerebro_ai_impact_function", "cerebro_ai_impact_operating_loop", "cerebro_ai_impact_project_binding", "cerebro_ai_impact_metric", "cerebro_ai_impact_observation", "workspace_id", "evidence_status", "confidence", "prevent_ai_impact_observation_mutation"} {
		if !strings.Contains(sql, required) {
			t.Errorf("migration missing %q", required)
		}
	}
	if strings.Contains(strings.ToLower(sql), "illustrative") || strings.Contains(sql, "INSERT INTO cerebro_ai_impact_observation") {
		t.Fatal("must not seed observations")
	}
	down, err := os.ReadFile("../../../migrations/9145_cerebro_ai_impact.down.sql")
	if err != nil || !strings.Contains(string(down), "DROP TABLE IF EXISTS cerebro_ai_impact_function") {
		t.Fatal("invalid down migration")
	}
}
