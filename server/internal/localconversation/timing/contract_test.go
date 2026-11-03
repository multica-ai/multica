package timing

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

type contractFile struct {
	LegacyStages  []string            `json:"legacyStages"`
	Marks         []string            `json:"marks"`
	LegacyMapping map[string]string   `json:"legacyMapping"`
	Dimensions    map[string][]string `json:"dimensions"`
	Reasons       []string            `json:"reasons"`
	Records       []struct {
		Name     string          `json:"name"`
		TSOnly   bool            `json:"tsOnly"`
		Input    json.RawMessage `json:"input"`
		Expected json.RawMessage `json:"expected"`
	} `json:"records"`
}

func loadContract(t *testing.T) contractFile {
	t.Helper()
	raw, err := os.ReadFile("testdata/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var c contractFile
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestContractListsEveryMarkAndClosedEnum(t *testing.T) {
	t.Parallel()
	c := loadContract(t)
	for _, name := range append(append([]string{}, c.Marks...), c.LegacyStages...) {
		if !stages[name] {
			t.Errorf("stage %q is in the contract but this writer rejects it", name)
		}
	}
	if len(stages) != len(unique(append(append([]string{}, c.Marks...), c.LegacyStages...))) {
		t.Errorf("writer accepts %d stages, contract lists %d", len(stages), len(unique(append(append([]string{}, c.Marks...), c.LegacyStages...))))
	}
	got := map[string][]string{"origin": setList(origins), "clientKind": setList(clientKinds), "connection": setList(connections), "provider": setList(providers), "prepareKind": setList(prepareKinds), "turnKind": setList(turnKinds)}
	for name, want := range c.Dimensions {
		if !slices.Equal(got[name], sorted(want)) {
			t.Errorf("dimension %s = %v, contract %v", name, got[name], sorted(want))
		}
	}
	if !slices.Equal(setList(reasons), sorted(c.Reasons)) {
		t.Errorf("reasons = %v, contract %v", setList(reasons), sorted(c.Reasons))
	}
}

func TestContractSamples(t *testing.T) {
	t.Parallel()
	for _, sample := range loadContract(t).Records {
		if sample.TSOnly {
			continue
		}
		var r Record
		if err := json.Unmarshal(sample.Input, &r); err != nil {
			t.Fatalf("%s: %v", sample.Name, err)
		}
		clean, ok := Sanitize(r)
		if string(sample.Expected) == "null" {
			if ok {
				t.Errorf("%s: accepted %+v", sample.Name, clean)
			}
			continue
		}
		if !ok {
			t.Errorf("%s: rejected", sample.Name)
			continue
		}
		data, err := json.Marshal(clean)
		if err != nil {
			t.Fatal(err)
		}
		if !jsonEqual(data, sample.Expected) {
			t.Errorf("%s:\n got %s\nwant %s", sample.Name, data, sample.Expected)
		}
	}
}

func unique(in []string) []string { return slices.Compact(sorted(in)) }

func sorted(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

func setList(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}
