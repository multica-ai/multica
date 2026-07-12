package analytics

import (
	"fmt"
	"sort"
	"time"
)

const (
	DefaultPageLimit = 100
	MaxPageLimit     = 1000
)

func (q *Query) Normalize() error {
	if !contains(allPopulations, q.Population) {
		return fmt.Errorf("analytics: invalid population %q", q.Population)
	}
	if len(q.Metrics) == 0 {
		q.Metrics = []Metric{MetricRuns}
	}
	q.Metrics = unique(q.Metrics)
	for _, metric := range q.Metrics {
		if !contains(allMetrics, metric) {
			return fmt.Errorf("analytics: invalid metric %q", metric)
		}
	}
	q.Dimensions = unique(q.Dimensions)
	for _, dimension := range q.Dimensions {
		if !contains(allDimensions, dimension) {
			return fmt.Errorf("analytics: invalid dimension %q", dimension)
		}
	}
	if q.Grain == "" {
		q.Grain = GrainNone
	}
	if !contains(allGrains, q.Grain) {
		return fmt.Errorf("analytics: invalid grain %q", q.Grain)
	}
	if q.Grain != GrainNone && !contains(q.Dimensions, DimensionTime) {
		q.Dimensions = append(q.Dimensions, DimensionTime)
	}
	if q.Page.Limit == 0 {
		q.Page.Limit = DefaultPageLimit
	}
	if q.Page.Limit < 1 || q.Page.Limit > MaxPageLimit {
		return fmt.Errorf("analytics: page limit must be between 1 and %d", MaxPageLimit)
	}
	if q.Timezone == "" {
		q.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(q.Timezone); err != nil {
		return fmt.Errorf("analytics: invalid timezone %q: %w", q.Timezone, err)
	}

	for i := range q.Filters {
		filter := &q.Filters[i]
		if !contains(allDimensions, filter.Dimension) {
			return fmt.Errorf("analytics: invalid filter dimension %q", filter.Dimension)
		}
		if !contains(allOperators, filter.Operator) {
			return fmt.Errorf("analytics: invalid filter operator %q", filter.Operator)
		}
		filter.Values = unique(filter.Values)
		sort.Strings(filter.Values)
		if len(filter.Values) == 0 {
			return fmt.Errorf("analytics: filter %q requires a value", filter.Dimension)
		}
		if (filter.Operator == OperatorEqual || filter.Operator == OperatorGreaterEqual || filter.Operator == OperatorLessEqual) && len(filter.Values) != 1 {
			return fmt.Errorf("analytics: operator %q requires exactly one value", filter.Operator)
		}
	}

	fields := make(map[string]struct{}, len(q.Metrics)+len(q.Dimensions))
	for _, metric := range q.Metrics {
		fields[string(metric)] = struct{}{}
	}
	for _, dimension := range q.Dimensions {
		fields[string(dimension)] = struct{}{}
	}
	for _, order := range q.Sort {
		if _, ok := fields[order.Field]; !ok {
			return fmt.Errorf("analytics: sort field %q is not selected", order.Field)
		}
		if order.Direction != SortAscending && order.Direction != SortDescending {
			return fmt.Errorf("analytics: invalid sort direction %q", order.Direction)
		}
	}
	return nil
}

func contains[T comparable](values []T, target T) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func unique[T comparable](values []T) []T {
	seen := make(map[T]struct{}, len(values))
	result := make([]T, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
