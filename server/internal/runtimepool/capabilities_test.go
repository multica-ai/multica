package runtimepool

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeAdvertisedCapabilitiesSortsAndDeduplicates(t *testing.T) {
	got, err := NormalizeAdvertisedCapabilities([]string{"z/v1", "a/v1", "a/v1"})
	if err != nil {
		t.Fatalf("NormalizeAdvertisedCapabilities: %v", err)
	}
	want := []string{"a/v1", "z/v1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized capabilities = %v, want %v", got, want)
	}
}

func TestNormalizeAdvertisedCapabilitiesPreservesExplicitEmpty(t *testing.T) {
	got, err := NormalizeAdvertisedCapabilities([]string{})
	if err != nil {
		t.Fatalf("NormalizeAdvertisedCapabilities: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("normalized capabilities = %#v, want non-nil empty slice", got)
	}
}

func TestNormalizeAdvertisedCapabilitiesRejectsInvalidBoundaries(t *testing.T) {
	tooMany := make([]string, MaxCapabilities+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("capability-%02d/v1", i)
	}
	cases := []struct {
		name       string
		advertised []string
	}{
		{name: "33 unique items", advertised: tooMany},
		{name: "129 bytes", advertised: []string{"a" + strings.Repeat("x", MaxCapabilityBytes)}},
		{name: "uppercase", advertised: []string{"A/v1"}},
		{name: "space", advertised: []string{"a capability/v1"}},
		{name: "non ASCII", advertised: []string{"capability/能力"}},
		{name: "empty", advertised: []string{""}},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NormalizeAdvertisedCapabilities(test.advertised); err == nil {
				t.Fatalf("NormalizeAdvertisedCapabilities(%q) succeeded", test.advertised)
			}
		})
	}
}

func TestContainsAllCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		have     []string
		required []string
		want     bool
	}{
		{name: "all present", have: []string{"a/v1", "b/v1"}, required: []string{"b/v1", "a/v1"}, want: true},
		{name: "missing", have: []string{"a/v1"}, required: []string{"a/v1", "b/v1"}, want: false},
		{name: "nothing required", have: nil, required: nil, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ContainsAllCapabilities(test.have, test.required); got != test.want {
				t.Fatalf("ContainsAllCapabilities(%v, %v) = %v, want %v", test.have, test.required, got, test.want)
			}
		})
	}
}
