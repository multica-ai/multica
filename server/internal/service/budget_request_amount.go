package service

import (
	"regexp"
	"strconv"
	"strings"
)

// budgetRequestKind is the knowledge kind of a budget request document (spec 4.2).
const budgetRequestKind = "budget_request"

var amountNumber = regexp.MustCompile(`\$?\s*([0-9][0-9,]*(?:\.[0-9]{1,2})?)`)

// parseBudgetRequestAmount reads the monthly amount a budget_request states, in
// micros of a US dollar. The amount is the first number under an "Amount" heading
// (the line after it, the first non-empty one) or on an "Amount:" line. It reports
// false when there is no such line or the number is not a positive amount, so the
// caller falls back to the default budget rather than guessing.
func parseBudgetRequestAmount(body string) (int64, bool) {
	lines := strings.Split(body, "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		var text string
		switch {
		case strings.HasPrefix(line, "#") && strings.EqualFold(strings.TrimSpace(strings.TrimLeft(line, "#")), "amount"):
			for _, next := range lines[i+1:] {
				if strings.TrimSpace(next) != "" {
					text = next
					break
				}
			}
		case strings.HasPrefix(strings.ToLower(line), "amount:"):
			text = line[len("amount:"):]
		default:
			continue
		}
		m := amountNumber.FindStringSubmatch(text)
		if m == nil {
			return 0, false
		}
		value, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
		if err != nil || value <= 0 {
			return 0, false
		}
		return int64(value*1_000_000 + 0.5), true
	}
	return 0, false
}
