package aiimpact

import (
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestMetricValidationRequiresBusinessEvidenceContract(t *testing.T) {
	metric := MetricInput{OperatingLoopID: uuid.New(), Name: "Needs solved", Family: FamilyOutcome, Unit: "needs", Direction: DirectionIncrease}
	if err := ValidateMetric(metric); err == nil {
		t.Fatal("expected missing baseline and source to fail")
	}
	metric.BaselineStart = time.Now().Add(-30 * 24 * time.Hour)
	metric.BaselineEnd = time.Now().Add(-24 * time.Hour)
	metric.Source = "firtal-ai-coach"
	metric.Guardrail = true
	if err := ValidateMetric(metric); err != nil {
		t.Fatalf("expected valid metric: %v", err)
	}
}

func TestObservationValidationProtectsConfidenceAndEvidence(t *testing.T) {
	input := ObservationInput{MetricID: uuid.New(), PeriodStart: time.Now().Add(-time.Hour), PeriodEnd: time.Now(), Value: .82, EvidenceStatus: EvidenceEstimated, Confidence: 1.2, Source: "coach", Method: "sampled"}
	if err := ValidateObservation(input); err == nil {
		t.Fatal("expected confidence above one to fail")
	}
	input.Confidence = .72
	if err := ValidateObservation(input); err != nil {
		t.Fatalf("expected valid observation: %v", err)
	}
}

func TestComputeDecisionSeparatesCashCapacityAndEstimatedValue(t *testing.T) {
	result := ComputeDecision(DecisionInput{RealizedCashCents: 120000, ApprovedCapacityCents: 45000, EstimatedValueCents: 300000, AICostCents: 20000, ImplementationCostCents: 10000, OutcomePositive: true, EvidenceMeasured: true, Guardrails: []GuardrailResult{{Passed: true}}})
	if result.NetValueCents != 135000 || result.EstimatedValueCents != 300000 || result.Decision != DecisionScale {
		t.Fatalf("unexpected result: %+v", result)
	}
	result = ComputeDecision(DecisionInput{Guardrails: []GuardrailResult{{Critical: true, Passed: false}}})
	if result.Decision != DecisionStop {
		t.Fatalf("critical failure must stop, got %s", result.Decision)
	}
}

func TestLatestObservationsDoesNotDoubleCountSameMetricPeriod(t *testing.T) {
	metricID := uuid.New()
	period := time.Now().UTC().Truncate(24 * time.Hour)
	rows := []Observation{{ID: uuid.New(), MetricID: metricID, PeriodStart: period, PeriodEnd: period.Add(24 * time.Hour), Value: 3, CreatedAt: period.Add(time.Hour)}, {ID: uuid.New(), MetricID: metricID, PeriodStart: period, PeriodEnd: period.Add(24 * time.Hour), Value: 4, CreatedAt: period.Add(2 * time.Hour)}}
	latest := LatestObservations(rows)
	if len(latest) != 1 || latest[0].Value != 4 {
		t.Fatalf("latest = %+v", latest)
	}
}

func TestCanConfigureOwnerAdminOnly(t *testing.T) {
	if !CanConfigure("owner") || !CanConfigure("admin") || CanConfigure("member") {
		t.Fatal("role matrix invalid")
	}
}
