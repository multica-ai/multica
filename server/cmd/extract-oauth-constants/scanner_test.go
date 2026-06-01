package main

import "testing"

func TestExtractCStrings(t *testing.T) {
	data := []byte("foo\x00bar\x00\x00qux\x01z\x00hello world")
	hits := extractCStrings(data, 3)
	want := []StringHit{
		{Offset: 0, Value: "foo"},
		{Offset: 4, Value: "bar"},
		{Offset: 9, Value: "qux"},
		{Offset: 15, Value: "hello world"},
	}
	if len(hits) != len(want) {
		t.Fatalf("hit count = %d, want %d: %+v", len(hits), len(want), hits)
	}
	for i, h := range hits {
		if h != want[i] {
			t.Errorf("hits[%d] = %+v, want %+v", i, h, want[i])
		}
	}
}

func TestExtractCStrings_RespectsMinLen(t *testing.T) {
	data := []byte("ab\x00cd\x00efgh\x00")
	hits := extractCStrings(data, 4)
	if len(hits) != 1 || hits[0].Value != "efgh" {
		t.Errorf("unexpected hits: %+v", hits)
	}
}

