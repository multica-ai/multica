package metrics

import "testing"

func TestRegistryExcludesDatabaseSampledMetrics(t *testing.T) {
	registry := NewRegistry(RegistryOptions{})
	families, err := registry.Gatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	retired := map[string]struct{}{
		"multica_agent_task_queued":                               {},
		"multica_agent_task_running":                              {},
		"multica_agent_task_stuck_total":                          {},
		"multica_business_sampler_query_errors_total":             {},
		"multica_business_sampler_query_seconds":                  {},
		"multica_workspace_total":                                 {},
		"multica_seat_capacity_outbox_pending":                    {},
		"multica_seat_capacity_outbox_dead_lettered":              {},
		"multica_seat_capacity_outbox_oldest_pending_age_seconds": {},
		"multica_channel_media_pending_objects":                   {},
		"multica_channel_media_tombstoned_objects":                {},
		"multica_runtime_gc_blocked_observation_failed_total":     {},
		"multica_runtime_gc_blocked_runtimes":                     {},
		"multica_runtime_gc_backlog_runtimes":                     {},
	}
	for _, family := range families {
		if _, found := retired[family.GetName()]; found {
			t.Errorf("retired database-sampled metric %q is still registered", family.GetName())
		}
	}
}
