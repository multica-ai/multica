package issueproperty

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestMultiSelectValueLimit(t *testing.T) {
	options := make([]propertyOption, 200)
	ids := make([]string, len(options))
	for i := range options {
		ids[i] = uuid.NewString()
		options[i] = propertyOption{ID: ids[i], Name: fmt.Sprintf("Option %d", i)}
	}
	config, err := json.Marshal(propertyConfig{Options: options})
	if err != nil {
		t.Fatal(err)
	}
	def := db.IssueProperty{Type: "multi_select", Config: config}

	for _, count := range []int{1, 50, 51, 200} {
		t.Run(fmt.Sprintf("%d distinct values", count), func(t *testing.T) {
			values := slices.Clone(ids[len(ids)-count:])
			slices.Reverse(values)
			raw, _ := json.Marshal(values)
			stored, err := ValidateValue(def, raw)
			if count > 50 {
				if err == nil || !strings.Contains(err.Error(), "more than 50 options") {
					t.Fatalf("expected selection limit error, got %s, %v", stored, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want, _ := json.Marshal(ids[len(ids)-count:])
			if string(stored) != string(want) {
				t.Fatalf("stored = %s, want config order %s", stored, want)
			}
		})
	}

	t.Run("duplicates retain existing canonicalization", func(t *testing.T) {
		values := slices.Repeat(ids[:50], 4)
		raw, _ := json.Marshal(values)
		stored, err := ValidateValue(def, raw)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := json.Marshal(ids[:50])
		if string(stored) != string(want) {
			t.Fatalf("stored = %s, want deduplicated %s", stored, want)
		}
	})
}

func TestSelectValueHintsAreBounded(t *testing.T) {
	for _, count := range []int{1, 20, 21, 200} {
		t.Run(fmt.Sprintf("%d options", count), func(t *testing.T) {
			options := make([]propertyOption, count)
			for i := range options {
				options[i] = propertyOption{ID: uuid.NewString(), Name: fmt.Sprintf("Option %d", i)}
			}
			config, _ := json.Marshal(propertyConfig{Options: options})
			for _, propertyType := range []string{"select", "multi_select"} {
				def := db.IssueProperty{Type: propertyType, Config: config}
				_, err := ValidateValue(def, json.RawMessage(`"unknown"`))
				if err == nil {
					t.Fatal("invalid selection accepted")
				}
				message := err.Error()
				for _, option := range options[:min(count, 20)] {
					if !strings.Contains(message, option.ID+" ("+option.Name+")") {
						t.Fatalf("%s error missing option %s: %s", propertyType, option.ID, message)
					}
				}
				if count > 20 {
					if strings.Contains(message, options[20].ID) || !strings.Contains(message, fmt.Sprintf("%d more options", count-20)) {
						t.Fatalf("%s error should truncate long lists: %s", propertyType, message)
					}
				}
				last := options[count-1].ID
				var value any = last
				if propertyType == "multi_select" {
					value = []string{last}
				}
				raw, _ := json.Marshal(value)
				if _, err := ValidateValue(def, raw); err != nil {
					t.Fatalf("%s rejected last valid option: %v", propertyType, err)
				}
			}
		})
	}
}
