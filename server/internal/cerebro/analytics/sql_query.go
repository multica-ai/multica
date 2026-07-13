package analytics

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type SQLPlan struct {
	SQL  string
	Args []any
}

func BuildSQL(query Query, workspaceID string) (SQLPlan, error) {
	if workspaceID == "" {
		return SQLPlan{}, fmt.Errorf("analytics: workspace is required")
	}
	if err := query.Normalize(); err != nil {
		return SQLPlan{}, err
	}

	args := []any{workspaceID}
	where := []string{"r.workspace_id = $1"}
	if query.Population != PopulationAll {
		args = append(args, string(query.Population))
		where = append(where, "r.population = $"+strconv.Itoa(len(args)))
	}

	dimensions := make([]string, 0, len(query.Dimensions))
	groups := make([]string, 0, len(query.Dimensions))
	for _, dimension := range query.Dimensions {
		expr, err := dimensionSQL(dimension, query.Grain, query.Timezone, &args)
		if err != nil {
			return SQLPlan{}, err
		}
		dimensions = append(dimensions, expr+" AS "+string(dimension))
		groups = append(groups, expr)
	}

	for _, filter := range query.Filters {
		args = append(args, filter.Values)
		expr, param, err := filterExpression(filter.Dimension, len(args))
		if err != nil {
			return SQLPlan{}, err
		}
		switch filter.Operator {
		case OperatorIn:
			where = append(where, expr+" = ANY("+param+")")
		case OperatorNotIn:
			where = append(where, "NOT ("+expr+" = ANY("+param+"))")
		case OperatorEqual:
			where = append(where, expr+" = ("+param+")[1]")
		case OperatorGreaterEqual:
			where = append(where, expr+" >= ("+param+")[1]")
		case OperatorLessEqual:
			where = append(where, expr+" <= ("+param+")[1]")
		}
	}
	offset := 0
	if query.Page.Cursor != "" {
		if parsedOffset, ok := parseOffsetCursor(query.Page.Cursor); ok {
			offset = parsedOffset
		} else {
			if _, err := time.Parse(time.RFC3339, query.Page.Cursor); err != nil {
				return SQLPlan{}, fmt.Errorf("analytics: invalid cursor: %w", err)
			}
			args = append(args, query.Page.Cursor)
			where = append(where, "r.started_at < $"+strconv.Itoa(len(args))+"::timestamptz")
		}
	}

	metrics := make([]string, 0, len(query.Metrics))
	for _, metric := range query.Metrics {
		expr, ok := metricSQL[metric]
		if !ok {
			return SQLPlan{}, fmt.Errorf("analytics: metric %q is not queryable yet", metric)
		}
		metrics = append(metrics, expr+" AS "+string(metric))
	}
	selects := append(dimensions, metrics...)
	joins := ""
	if contains(query.Metrics, MetricSavedCents) {
		joins += " LEFT JOIN cerebro_analytics_run_saving s ON s.analytics_run_id=r.id"
	}
	if contains(query.Metrics, MetricQualityPassRate) || queryUsesDimension(query, DimensionQualityType, DimensionQualityCategory) {
		joins += " LEFT JOIN cerebro_analytics_quality_measurement q ON q.analytics_run_id=r.id"
	}
	if contains(query.Metrics, MetricSkillInvocations) || queryUsesDimension(query, DimensionSkill) {
		joins += " LEFT JOIN cerebro_analytics_run_skill sk ON sk.analytics_run_id=r.id"
	}
	if queryUsesDimension(query, DimensionContext, DimensionReference, DimensionReferenceLabel, DimensionDebugLink) {
		joins += " LEFT JOIN cerebro_analytics_reference ref ON ref.analytics_run_id=r.id"
		if !queryUsesDimension(query, DimensionContext) {
			joins += " AND ref.href IS NOT NULL"
		}
	}
	sql := `SELECT ` + strings.Join(selects, ", ") + ` FROM cerebro_analytics_run r` + joins + ` WHERE ` + strings.Join(where, " AND ")
	if len(groups) > 0 {
		sql += " GROUP BY " + strings.Join(groups, ", ")
	}
	if len(query.Sort) > 0 {
		parts := make([]string, 0, len(query.Sort))
		for _, sort := range query.Sort {
			parts = append(parts, sort.Field+" "+strings.ToUpper(string(sort.Direction)))
		}
		sql += " ORDER BY " + strings.Join(parts, ", ")
	} else if contains(query.Dimensions, DimensionTime) {
		sql += " ORDER BY time DESC"
	} else if len(query.Metrics) > 0 {
		sql += " ORDER BY " + string(query.Metrics[0]) + " DESC"
	}
	sql += " LIMIT " + strconv.Itoa(query.Page.Limit+1)
	if offset > 0 {
		sql += " OFFSET " + strconv.Itoa(offset)
	}
	return SQLPlan{SQL: sql, Args: args}, nil
}

func queryUsesDimension(query Query, dimensions ...Dimension) bool {
	for _, dimension := range dimensions {
		if contains(query.Dimensions, dimension) {
			return true
		}
		for _, filter := range query.Filters {
			if filter.Dimension == dimension {
				return true
			}
		}
	}
	return false
}

func parseOffsetCursor(cursor string) (int, bool) {
	if !strings.HasPrefix(cursor, "offset:") {
		return 0, false
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(cursor, "offset:"))
	return offset, err == nil && offset >= 0
}

var filterDimensionSQL = map[Dimension]string{
	DimensionPerson: "r.person_label", DimensionAgent: "r.agent_label", DimensionProject: "r.project_label", DimensionRuntime: "r.runtime_label", DimensionSource: "r.source_type", DimensionProvider: "r.provider", DimensionModel: "r.model", DimensionSkill: "sk.skill_name", DimensionStatus: "r.status", DimensionCostKind: "r.cost_kind", DimensionQualityType: "q.measurement_type", DimensionQualityCategory: "q.category", DimensionContext: "ref.reference_kind",
	DimensionRun: "r.run_id::text", DimensionSourceID: "r.source_id::text", DimensionReference: "ref.reference_id", DimensionReferenceLabel: "ref.label", DimensionDebugLink: "ref.href", DimensionTrace: "r.trace_id",
}

func filterExpression(d Dimension, argIndex int) (string, string, error) {
	if d == DimensionTime {
		return "r.started_at", "$" + strconv.Itoa(argIndex) + "::timestamptz[]", nil
	}
	expr, ok := filterDimensionSQL[d]
	if !ok {
		return "", "", fmt.Errorf("analytics: dimension %q is not queryable yet", d)
	}
	return expr, "$" + strconv.Itoa(argIndex) + "::text[]", nil
}

func dimensionSQL(d Dimension, grain Grain, timezone string, args *[]any) (string, error) {
	if d == DimensionTime {
		if grain == GrainNone {
			return "r.started_at", nil
		}
		*args = append(*args, timezone)
		return "date_trunc('" + string(grain) + "', r.started_at AT TIME ZONE $" + strconv.Itoa(len(*args)) + ")", nil
	}
	if expr, ok := filterDimensionSQL[d]; ok {
		return expr, nil
	}
	return "", fmt.Errorf("analytics: dimension %q is not queryable yet", d)
}

var metricSQL = map[Metric]string{
	MetricRuns: "COUNT(DISTINCT r.run_id)::bigint", MetricInputTokens: "SUM(r.input_tokens)::bigint", MetricOutputTokens: "SUM(r.output_tokens)::bigint", MetricCostCents: "SUM(r.cost_cents)::bigint", MetricMissingCostRuns: "COUNT(DISTINCT r.run_id) FILTER (WHERE r.cost_kind = 'missing')::bigint", MetricSavedCents: "SUM(s.saved_cents)::bigint", MetricDurationSeconds: "SUM(r.duration_seconds)::bigint", MetricQualityPassRate: "AVG(CASE WHEN q.verdict IN ('pass','success') THEN 1.0 ELSE 0.0 END)", MetricSkillInvocations: "SUM(sk.invocation_count)::bigint",
}
