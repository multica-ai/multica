package search

import (
	"reflect"
	"testing"
)

func TestParse_PlainText(t *testing.T) {
	p := Parse("login bug")
	if p.Text != "login bug" {
		t.Errorf("Text = %q, want %q", p.Text, "login bug")
	}
	if len(p.Filters) != 0 {
		t.Errorf("Filters = %+v, want none", p.Filters)
	}
}

func TestParse_FromFilter(t *testing.T) {
	p := Parse("login bug from:jesper")
	if p.Text != "login bug" {
		t.Errorf("Text = %q, want %q", p.Text, "login bug")
	}
	want := []Filter{{Key: FilterFrom, Value: "jesper"}}
	if !reflect.DeepEqual(p.Filters, want) {
		t.Errorf("Filters = %+v, want %+v", p.Filters, want)
	}
}

func TestParse_MultipleFilters(t *testing.T) {
	p := Parse("deploy from:jesper status:todo project:lager has:attachment")
	if p.Text != "deploy" {
		t.Errorf("Text = %q, want %q", p.Text, "deploy")
	}
	want := []Filter{
		{Key: FilterFrom, Value: "jesper"},
		{Key: FilterStatus, Value: "todo"},
		{Key: FilterProject, Value: "lager"},
		{Key: FilterHas, Value: "attachment"},
	}
	if !reflect.DeepEqual(p.Filters, want) {
		t.Errorf("Filters = %+v, want %+v", p.Filters, want)
	}
}

func TestParse_QuotedValue(t *testing.T) {
	p := Parse(`deploy project:"my project" status:todo`)
	if p.Text != "deploy" {
		t.Errorf("Text = %q, want %q", p.Text, "deploy")
	}
	want := []Filter{
		{Key: FilterProject, Value: "my project"},
		{Key: FilterStatus, Value: "todo"},
	}
	if !reflect.DeepEqual(p.Filters, want) {
		t.Errorf("Filters = %+v, want %+v", p.Filters, want)
	}
}

func TestParse_SingleQuotedValue(t *testing.T) {
	p := Parse(`from:'mia hansen' bug`)
	if p.Text != "bug" {
		t.Errorf("Text = %q, want %q", p.Text, "bug")
	}
	if len(p.Filters) != 1 || p.Filters[0].Value != "mia hansen" {
		t.Errorf("Filters = %+v, want one from:'mia hansen'", p.Filters)
	}
}

func TestParse_FilterAtStart(t *testing.T) {
	p := Parse("from:jesper login bug")
	if p.Text != "login bug" {
		t.Errorf("Text = %q, want %q", p.Text, "login bug")
	}
	if len(p.Filters) != 1 || p.Filters[0].Key != FilterFrom {
		t.Errorf("Filters = %+v", p.Filters)
	}
}

func TestParse_NoText(t *testing.T) {
	p := Parse("status:todo assignee:jesper")
	if p.Text != "" {
		t.Errorf("Text = %q, want empty", p.Text)
	}
	if len(p.Filters) != 2 {
		t.Errorf("Filters = %+v, want 2", p.Filters)
	}
}

func TestParse_UnknownPrefixKeptAsText(t *testing.T) {
	p := Parse("https://example.com bug from:jesper")
	if p.Text != "https://example.com bug" {
		t.Errorf("Text = %q, want url+bug as text", p.Text)
	}
	if len(p.Filters) != 1 || p.Filters[0].Value != "jesper" {
		t.Errorf("Filters = %+v", p.Filters)
	}
}

func TestParse_EmptyFilterValueDropped(t *testing.T) {
	p := Parse("from: bug")
	if p.Text != "bug" {
		t.Errorf("Text = %q, want %q", p.Text, "bug")
	}
	if len(p.Filters) != 0 {
		t.Errorf("bare `from:` should be dropped, got %+v", p.Filters)
	}
}

func TestParse_CaseInsensitiveKey(t *testing.T) {
	p := Parse("FROM:jesper STATUS:Todo")
	want := []Filter{
		{Key: FilterFrom, Value: "jesper"},
		{Key: FilterStatus, Value: "Todo"},
	}
	if !reflect.DeepEqual(p.Filters, want) {
		t.Errorf("Filters = %+v, want %+v", p.Filters, want)
	}
}

func TestParse_RepeatedKeyAccumulates(t *testing.T) {
	p := Parse("bug from:jesper from:mia")
	values := p.Values(FilterFrom)
	want := []string{"jesper", "mia"}
	if !reflect.DeepEqual(values, want) {
		t.Errorf("Values(from) = %v, want %v", values, want)
	}
}

func TestParse_UnbalancedQuote(t *testing.T) {
	// Unclosed quote should swallow the rest of the input as the value,
	// rather than dropping characters.
	p := Parse(`project:"my project deploy`)
	if len(p.Filters) != 1 {
		t.Fatalf("Filters = %+v, want one", p.Filters)
	}
	if p.Filters[0].Value != "my project deploy" {
		t.Errorf("Value = %q, want unclosed scan to end of input", p.Filters[0].Value)
	}
}

func TestStatusMap_KnownAliases(t *testing.T) {
	cases := map[string][]string{
		"open":        {"todo", "in_progress", "in_review", "blocked", "backlog"},
		"closed":      {"done", "cancelled"},
		"active":      {"in_progress", "in_review"},
		"todo":        {"todo"},
		"in-progress": {"in_progress"},
		"doing":       {"in_progress"},
		"review":      {"in_review"},
		"canceled":    {"cancelled"},
	}
	for in, want := range cases {
		got := StatusMap(in)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("StatusMap(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestStatusMap_Unknown(t *testing.T) {
	if got := StatusMap("nope"); got != nil {
		t.Errorf("StatusMap(nope) = %v, want nil", got)
	}
}

func TestHasFilter(t *testing.T) {
	p := Parse("bug has:attachment")
	if !p.HasFilter(FilterHas) {
		t.Error("HasFilter(has) = false, want true")
	}
	if p.HasFilter(FilterFrom) {
		t.Error("HasFilter(from) = true, want false")
	}
}
