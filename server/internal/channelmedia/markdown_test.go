package channelmedia

import (
	"reflect"
	"strings"
	"testing"
)

func TestBlockCarriesInvisibleProvenance(t *testing.T) {
	const id = "22222222-2222-4222-8222-222222222222"
	block := Block(id, "diagram.png", true)
	if !strings.Contains(block, "![]("+DownloadPath(id)+")") {
		t.Fatalf("image block = %q", block)
	}
	if !HasMarker(block, id) {
		t.Fatalf("image block has no provenance marker: %q", block)
	}
	if got := MarkedIDs(block); !reflect.DeepEqual(got, []string{id}) {
		t.Fatalf("marked ids = %v, want [%s]", got, id)
	}
}

func TestBlockEscapesFileLabelAndFlattensNewlines(t *testing.T) {
	const id = "33333333-3333-4333-8333-333333333333"
	block := Block(id, "a[b]\nreport.pdf", false)
	wantLink := "[a\\[b\\] report.pdf](" + DownloadPath(id) + ")"
	if !strings.Contains(block, wantLink) {
		t.Fatalf("file block = %q, want link %q", block, wantLink)
	}
}

func TestMarkedIDsIgnoresInvalidAndDeduplicates(t *testing.T) {
	const id = "44444444-4444-4444-8444-444444444444"
	markdown := Marker(id) + "\n" + Marker("not-a-uuid") + "\n" + Marker(id)
	if got := MarkedIDs(markdown); !reflect.DeepEqual(got, []string{id}) {
		t.Fatalf("marked ids = %v, want [%s]", got, id)
	}
}
