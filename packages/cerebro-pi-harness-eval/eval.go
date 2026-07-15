package piharnesseval

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed cases.json
var casesJSON []byte

type Check struct {
	Name    string   `json:"name"`
	Dir     string   `json:"dir"`
	Command []string `json:"command"`
}

type Delivery struct {
	ID       string  `json:"id"`
	Delivery string  `json:"delivery"`
	Checks   []Check `json:"checks"`
}

type CheckResult struct {
	Name       string `json:"name"`
	Passed     bool   `json:"passed"`
	DurationMS int64  `json:"duration_ms"`
	Output     string `json:"output,omitempty"`
}

type DeliveryResult struct {
	ID       string        `json:"id"`
	Delivery string        `json:"delivery"`
	Passed   bool          `json:"passed"`
	Checks   []CheckResult `json:"checks"`
}

type Report struct {
	Passed             bool             `json:"passed"`
	RequiredDeliveries int              `json:"required_deliveries"`
	PassedDeliveries   int              `json:"passed_deliveries"`
	GeneratedAt        string           `json:"generated_at,omitempty"`
	Results            []DeliveryResult `json:"results"`
}

func LoadCases() ([]Delivery, error) {
	var deliveries []Delivery
	if err := json.Unmarshal(casesJSON, &deliveries); err != nil {
		return nil, err
	}
	return deliveries, nil
}

func Score(required []string, results []DeliveryResult) Report {
	byID := make(map[string]DeliveryResult, len(results))
	for i := range results {
		result := &results[i]
		passed := len(result.Checks) > 0
		for _, check := range result.Checks {
			passed = passed && check.Passed
		}
		result.Passed = passed
		byID[result.ID] = *result
	}
	report := Report{RequiredDeliveries: len(required), Results: results}
	for _, id := range required {
		if result, ok := byID[id]; ok && result.Passed {
			report.PassedDeliveries++
		}
	}
	report.Passed = report.RequiredDeliveries > 0 && report.PassedDeliveries == report.RequiredDeliveries
	return report
}

func Run(ctx context.Context, repoRoot string, deliveries []Delivery) Report {
	results := make([]DeliveryResult, 0, len(deliveries))
	required := make([]string, 0, len(deliveries))
	for _, delivery := range deliveries {
		required = append(required, delivery.ID)
		result := DeliveryResult{ID: delivery.ID, Delivery: delivery.Delivery}
		for _, check := range delivery.Checks {
			started := time.Now()
			command := exec.CommandContext(ctx, check.Command[0], check.Command[1:]...)
			command.Dir = filepath.Join(repoRoot, check.Dir)
			output, err := command.CombinedOutput()
			result.Checks = append(result.Checks, CheckResult{
				Name: check.Name, Passed: err == nil, DurationMS: time.Since(started).Milliseconds(), Output: strings.TrimSpace(string(output)),
			})
		}
		results = append(results, result)
	}
	report := Score(required, results)
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	return report
}

func Markdown(report Report) string {
	status := "FAIL"
	if report.Passed {
		status = "PASS"
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# Pi Harness delivery eval — %s\n\n", status)
	fmt.Fprintf(&out, "%d/%d deliveries passed. Threshold: every delivery and every check must pass.\n\n", report.PassedDeliveries, report.RequiredDeliveries)
	out.WriteString("| Delivery | Result | Checks |\n|---|---|---|\n")
	for _, result := range report.Results {
		checkNames := make([]string, 0, len(result.Checks))
		for _, check := range result.Checks {
			mark := "PASS"
			if !check.Passed {
				mark = "FAIL"
			}
			checkNames = append(checkNames, check.Name+" ("+mark+")")
		}
		resultStatus := "FAIL"
		if result.Passed {
			resultStatus = "PASS"
		}
		fmt.Fprintf(&out, "| %s — %s | %s | %s |\n", result.ID, result.Delivery, resultStatus, strings.Join(checkNames, "; "))
	}
	return out.String()
}
