package analytics

type Population string

const (
	PopulationAgent   Population = "agent"
	PopulationGateway Population = "gateway"
	PopulationAll     Population = "all"
)

type Metric string

const (
	MetricRuns             Metric = "runs"
	MetricInputTokens      Metric = "input_tokens"
	MetricOutputTokens     Metric = "output_tokens"
	MetricCostCents        Metric = "cost_cents"
	MetricSavedCents       Metric = "saved_cents"
	MetricDurationSeconds  Metric = "duration_seconds"
	MetricQualityPassRate  Metric = "quality_pass_rate"
	MetricSkillInvocations Metric = "skill_invocations"
)

type Dimension string

const (
	DimensionTime            Dimension = "time"
	DimensionPerson          Dimension = "person"
	DimensionAgent           Dimension = "agent"
	DimensionProject         Dimension = "project"
	DimensionSource          Dimension = "source"
	DimensionProvider        Dimension = "provider"
	DimensionModel           Dimension = "model"
	DimensionSkill           Dimension = "skill"
	DimensionStatus          Dimension = "status"
	DimensionQualityType     Dimension = "quality_type"
	DimensionQualityCategory Dimension = "quality_category"
	DimensionContext         Dimension = "context"
	DimensionRun             Dimension = "run"
	DimensionSourceID        Dimension = "source_id"
	DimensionReference       Dimension = "reference"
	DimensionReferenceLabel  Dimension = "reference_label"
	DimensionDebugLink       Dimension = "debug_link"
	DimensionTrace           Dimension = "trace"
)

type Grain string

const (
	GrainNone  Grain = "none"
	GrainHour  Grain = "hour"
	GrainDay   Grain = "day"
	GrainWeek  Grain = "week"
	GrainMonth Grain = "month"
)

type Operator string

const (
	OperatorIn           Operator = "in"
	OperatorNotIn        Operator = "not_in"
	OperatorEqual        Operator = "eq"
	OperatorGreaterEqual Operator = "gte"
	OperatorLessEqual    Operator = "lte"
)

type SortDirection string

const (
	SortAscending  SortDirection = "asc"
	SortDescending SortDirection = "desc"
)

type Filter struct {
	Dimension Dimension `json:"dimension"`
	Operator  Operator  `json:"operator"`
	Values    []string  `json:"values"`
}

type Sort struct {
	Field     string        `json:"field"`
	Direction SortDirection `json:"direction"`
}

type Page struct {
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type Query struct {
	Population Population  `json:"population"`
	Metrics    []Metric    `json:"metrics,omitempty"`
	Dimensions []Dimension `json:"dimensions,omitempty"`
	Filters    []Filter    `json:"filters,omitempty"`
	Grain      Grain       `json:"grain,omitempty"`
	Sort       []Sort      `json:"sort,omitempty"`
	Page       Page        `json:"page,omitempty"`
	Timezone   string      `json:"timezone,omitempty"`
}

type Catalog struct {
	Populations []Population `json:"populations"`
	Metrics     []Metric     `json:"metrics"`
	Dimensions  []Dimension  `json:"dimensions"`
	Grains      []Grain      `json:"grains"`
	Operators   []Operator   `json:"operators"`
}

func ContractCatalog() Catalog {
	return Catalog{
		Populations: allPopulations,
		Metrics:     allMetrics,
		Dimensions:  allDimensions,
		Grains:      allGrains,
		Operators:   allOperators,
	}
}

var allPopulations = []Population{PopulationAgent, PopulationGateway, PopulationAll}
var allMetrics = []Metric{MetricRuns, MetricInputTokens, MetricOutputTokens, MetricCostCents, MetricSavedCents, MetricDurationSeconds, MetricQualityPassRate, MetricSkillInvocations}
var allDimensions = []Dimension{DimensionTime, DimensionPerson, DimensionAgent, DimensionProject, DimensionSource, DimensionProvider, DimensionModel, DimensionSkill, DimensionStatus, DimensionQualityType, DimensionQualityCategory, DimensionContext, DimensionRun, DimensionSourceID, DimensionReference, DimensionReferenceLabel, DimensionDebugLink, DimensionTrace}
var allGrains = []Grain{GrainNone, GrainHour, GrainDay, GrainWeek, GrainMonth}
var allOperators = []Operator{OperatorIn, OperatorNotIn, OperatorEqual, OperatorGreaterEqual, OperatorLessEqual}
