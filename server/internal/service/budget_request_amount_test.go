package service

import "testing"

// TestParseBudgetRequestAmountReadsTheAmountSection: the amount under the Amount
// heading or on an Amount line is read in micros; anything else is no amount.
func TestParseBudgetRequestAmountReadsTheAmountSection(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int64
		ok   bool
	}{
		{"heading then a dollar figure", "## Amount\n\n$1,200 per month\n\n## Justification\nWhy", 1_200_000_000, true},
		{"heading with decimals", "## Amount\n250.5 USD a month", 250_500_000, true},
		{"amount line", "Amount: $75\n\nJustification", 75_000_000, true},
		{"no amount section", "## Justification\nNo number here", 0, false},
		{"heading with no figure", "## Amount\nto be decided", 0, false},
		{"zero is not an amount", "## Amount\n$0", 0, false},
		{"the heading is not the first word", "Some text\n## Amounts\n$5", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseBudgetRequestAmount(c.body)
			if ok != c.ok || got != c.want {
				t.Fatalf("parseBudgetRequestAmount = %d, %v; want %d, %v", got, ok, c.want, c.ok)
			}
		})
	}
}
