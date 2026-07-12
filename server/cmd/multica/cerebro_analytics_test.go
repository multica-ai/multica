package main

import (
	"strings"
	"testing"
)

func TestLoadAnalyticsQueryPreservesCanonicalContract(t *testing.T) {
	query, err := loadAnalyticsQuery(strings.NewReader(`{"population":"agent","metrics":["runs","cost_cents"],"dimensions":["model"],"filters":[{"dimension":"status","operator":"not_in","values":["failed"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if query.Population != "agent" || len(query.Metrics) != 2 || query.Filters[0].Operator != "not_in" {
		t.Fatalf("query = %#v", query)
	}
}

func TestLoadAnalyticsQueryRejectsUnknownFields(t *testing.T) {
	if _, err := loadAnalyticsQuery(strings.NewReader(`{"population":"all","unknown":true}`)); err == nil {
		t.Fatal("loadAnalyticsQuery() error = nil")
	}
}
