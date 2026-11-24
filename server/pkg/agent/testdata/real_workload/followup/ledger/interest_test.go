package ledger

import (
	"reflect"
	"testing"
)

func TestApplyInterestRoundsHalfToEven(t *testing.T) {
	in := Book{"a": 12345, "b": 100, "c": 300, "d": 0, "e": 250}
	got := ApplyInterest(in, 150)
	want := Book{"a": 12530, "b": 102, "c": 304, "d": 0, "e": 254}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ApplyInterest(150bp) = %v, want %v", got, want)
	}
	half := ApplyInterest(Book{"x": 100, "y": 300, "z": 500}, 50)
	if want := (Book{"x": 100, "y": 302, "z": 502}); !reflect.DeepEqual(half, want) {
		t.Fatalf("ApplyInterest(50bp) = %v, want %v (0.5 -> 0, 1.5 -> 2, 2.5 -> 2)", half, want)
	}
	if in["a"] != 12345 {
		t.Fatal("ApplyInterest must not modify its input")
	}
}

func TestInterestThenReport(t *testing.T) {
	got := Report(ApplyInterest(Book{"alice": 100000000}, 25))
	want := "" +
		"alice      1,002,500.00\n" +
		"TOTAL      1,002,500.00\n"
	if got != want {
		t.Fatalf("Report =\n%s\nwant\n%s", got, want)
	}
}
