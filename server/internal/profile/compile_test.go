package profile

import (
	"strings"
	"testing"
)

// These golden strings MUST match the TS snapshots in
// packages/core/profile/__snapshots__/compile.test.ts.snap. If a snapshot
// changes, port the change here. Both implementations are the source of
// truth for the same product behaviour — diverging silently means the user
// sees one prompt in the UI preview and a different one at injection time.

func TestCompileUtalmodigDanish(t *testing.T) {
	got := Compile(Profile{
		Persona:      "utalmodig",
		Language:     "da",
		DisplayName:  "Jens",
		LengthPref:   15,
		AutonomyPref: 85,
		TechPref:     70,
		AntiPatterns: []string{
			`"Let me know if you need anything else"`,
			`"Great question!"`,
			"Tids-estimater",
		},
	})
	want := `USER: Jens (Den utålmodige, dansk)

STYLE:
- Default 1–3 sætninger. Vis data i tabel/liste hvis >5 punkter.
- Spring hilsner og "let me know"-closings over.
- Match navne fra koden eksakt. Ingen opfundne ord.

AUTONOMY:
- Reversibelt: bare kør, vis hvad der skete.
- Destruktivt: spørg først.
- Spørg ikke om ting du selv kan finde/gøre.

TECH: arch:deep, data:deep, ux:deep, code:deep

AVOID:
- "Let me know if you need anything else"
- "Great question!"
- Tids-estimater`

	if got != want {
		t.Fatalf("compile mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestCompileUtalmodigEnglish(t *testing.T) {
	got := Compile(Profile{
		Persona:      "utalmodig",
		Language:     "en",
		DisplayName:  "Jens",
		LengthPref:   15,
		AutonomyPref: 85,
		TechPref:     70,
		AntiPatterns: []string{
			`"Let me know if you need anything else"`,
			`"Great question!"`,
			"Time estimates",
		},
	})
	want := `USER: Jens (The impatient, english)

STYLE:
- Default 1–3 sentences. Use a table/list for >5 items.
- Skip greetings and "let me know"-closings.
- Match names from the code exactly. No invented terms.

AUTONOMY:
- Reversible: just do it, show what happened.
- Destructive: ask first.
- Don't ask about things you can find or do yourself.

TECH: arch:deep, data:deep, ux:deep, code:deep

AVOID:
- "Let me know if you need anything else"
- "Great question!"
- Time estimates`

	if got != want {
		t.Fatalf("compile mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestCompileWithoutDisplayNameSkipsParenthesisForm(t *testing.T) {
	got := Compile(Profile{
		Persona:      "grundig",
		Language:     "da",
		LengthPref:   75,
		AutonomyPref: 30,
		TechPref:     80,
	})
	if !strings.HasPrefix(got, "USER: Den grundige, dansk\n\n") {
		t.Fatalf("expected anonymous header, got: %q", got)
	}
}

func TestCompileEmptyAntiPatternsOmitsAvoidSection(t *testing.T) {
	got := Compile(Profile{
		Persona:  "ekspert",
		Language: "da",
	})
	if strings.Contains(got, "AVOID") {
		t.Fatalf("AVOID section should be omitted when no anti-patterns; got: %q", got)
	}
}

func TestCompileFromRowDecodesAntiPatterns(t *testing.T) {
	got, err := CompileFromRow("utalmodig", "da", "Jens", 15, 85, 70, []byte(`["foo","bar"]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "- foo\n- bar") {
		t.Fatalf("expected anti-patterns to appear; got: %q", got)
	}
}

func TestCompileFromRowMalformedAntiPatternsReturnsError(t *testing.T) {
	_, err := CompileFromRow("utalmodig", "da", "Jens", 15, 85, 70, []byte(`{"not": "an array"}`))
	if err == nil {
		t.Fatal("expected error for non-array anti_patterns JSON")
	}
}

func TestCompileSliderBucketsBoundaries(t *testing.T) {
	cases := []struct {
		name        string
		lengthPref  int
		wantInLine0 string
	}{
		{"low end 0", 0, "1–3 sætninger"},
		{"low boundary 33", 33, "1–3 sætninger"},
		{"mid boundary 34", 34, "3–6 sætninger"},
		{"mid boundary 66", 66, "3–6 sætninger"},
		{"high boundary 67", 67, "Forklar ræsonnement"},
		{"high end 100", 100, "Forklar ræsonnement"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := Compile(Profile{Persona: "grundig", Language: "da", LengthPref: c.lengthPref})
			if !strings.Contains(out, c.wantInLine0) {
				t.Fatalf("lengthPref=%d expected %q in output:\n%s", c.lengthPref, c.wantInLine0, out)
			}
		})
	}
}
