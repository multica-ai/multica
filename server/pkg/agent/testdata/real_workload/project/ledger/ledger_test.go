package ledger

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const journal = `# opening balances
2026-01-02 deposit alice 1200.50
2026-01-02 deposit bob 75

2026-01-03 transfer alice bob 200.25
2026-01-04 withdraw bob 300
2026-01-05 withdraw carol 1
2026-01-06 transfer bob alice 275.25
2026-01-07 deposit carol 1000000.01
`

func TestParse(t *testing.T) {
	txns, err := Parse(strings.NewReader(journal))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(txns) != 7 {
		t.Fatalf("parsed %d transactions, want 7", len(txns))
	}
	first := txns[0]
	if first.Line != 2 || first.Kind != Deposit || first.To != "alice" || first.Cents != 120050 || first.Date.Format("2006-01-02") != "2026-01-02" {
		t.Fatalf("first = %+v", first)
	}
	tr := txns[2]
	if tr.Line != 5 || tr.Kind != Transfer || tr.From != "alice" || tr.To != "bob" || tr.Cents != 20025 {
		t.Fatalf("transfer = %+v", tr)
	}
	w := txns[3]
	if w.Kind != Withdraw || w.From != "bob" || w.To != "" || w.Cents != 30000 {
		t.Fatalf("withdraw = %+v", w)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		src  string
		line int
		msg  string
	}{
		{"2026-01-02 deposit alice 1.234", 1, "amount"},
		{"2026-01-02 deposit alice -5", 1, "amount"},
		{"2026-01-02 deposit alice 0", 1, "amount"},
		{"# ok\n2026-13-02 deposit alice 5", 2, "date"},
		{"2026-01-02 refund alice 5", 1, "kind"},
		{"2026-01-02 transfer alice 5", 1, "fields"},
		{"2026-01-02 transfer alice alice 5", 1, "same account"},
		{"2026-01-02 deposit alice 5 extra", 1, "fields"},
	}
	for _, c := range cases {
		_, err := Parse(strings.NewReader(c.src))
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Fatalf("Parse(%q) error = %v, want *ParseError", c.src, err)
		}
		if pe.Line != c.line || !strings.Contains(pe.Msg, c.msg) {
			t.Fatalf("Parse(%q) = line %d %q, want line %d containing %q", c.src, pe.Line, pe.Msg, c.line, c.msg)
		}
		if !strings.HasPrefix(pe.Error(), "line ") {
			t.Fatalf("ParseError.Error() = %q", pe.Error())
		}
	}
}

func TestApply(t *testing.T) {
	txns, err := Parse(strings.NewReader(journal))
	if err != nil {
		t.Fatal(err)
	}
	book, rejected := Apply(txns)
	wantBook := Book{"alice": 120050 - 20025 + 27525, "bob": 7500 + 20025 - 27525, "carol": 100000001}
	if !reflect.DeepEqual(book, wantBook) {
		t.Fatalf("book = %v, want %v", book, wantBook)
	}
	wantRejected := []Rejection{
		{Line: 6, Reason: "insufficient funds"},
		{Line: 7, Reason: "unknown account"},
	}
	if !reflect.DeepEqual(rejected, wantRejected) {
		t.Fatalf("rejected = %+v, want %+v", rejected, wantRejected)
	}
}

func TestApplyTransferToNewAccount(t *testing.T) {
	txns, err := Parse(strings.NewReader("2026-02-01 deposit a 10\n2026-02-02 transfer a z 4"))
	if err != nil {
		t.Fatal(err)
	}
	book, rejected := Apply(txns)
	if len(rejected) != 0 || book["a"] != 600 || book["z"] != 400 {
		t.Fatalf("book=%v rejected=%v", book, rejected)
	}
}

func TestReport(t *testing.T) {
	book := Book{"carol": 100000001, "alice": 127550, "bob": 0}
	want := "" +
		"alice          1,275.50\n" +
		"bob                0.00\n" +
		"carol      1,000,000.01\n" +
		"TOTAL      1,001,275.51\n"
	if got := Report(book); got != want {
		t.Fatalf("Report() =\n%s\nwant\n%s", got, want)
	}
}
