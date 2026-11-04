package service

import (
	"strings"
	"testing"
)

func TestStarterSkillsForRole(t *testing.T) {
	t.Parallel()
	card := companySquad{Key: "market_research", Leader: "research_lead"}

	cases := []struct {
		name string
		card companySquad
		role companyRole
		want []string
	}{
		{
			name: "the Secretary gets the analysis skill only",
			card: companySquad{Key: "executive_office", Leader: "secretary"},
			role: companyRole{Key: "secretary", Runtime: "secretary"},
			want: []string{StarterSkillSecretaryAnalysis},
		},
		{
			name: "a squad leader gets the reporting and directive skills",
			card: card,
			role: companyRole{Key: "research_lead", Runtime: "worker"},
			want: []string{StarterSkillSquadReporting, StarterSkillDirectiveExecution},
		},
		{
			name: "a member gets none",
			card: card,
			role: companyRole{Key: "scout", Runtime: "worker"},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := starterSkillsForRole(tc.card, tc.role)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("starterSkillsForRole() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsStarterSkillConfig(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		`{"source":"starter_company"}`: true,
		`{"source":"someone_else"}`:    false,
		`{}`:                           false,
		`null`:                         false,
		``:                             false,
		`not json`:                     false,
		string(starterSkillConfig()):   true,
	}
	for in, want := range cases {
		if got := isStarterSkillConfig([]byte(in)); got != want {
			t.Errorf("isStarterSkillConfig(%q) = %v, want %v", in, got, want)
		}
	}
}

// The embedded data carries the three starter skills, each with a SKILL.md whose
// frontmatter name matches its folder, a description, a body, and reference files.
func TestStarterSkillsEmbeddedInCompanyData(t *testing.T) {
	t.Parallel()
	data, err := loadCompanyData()
	if err != nil {
		t.Fatalf("loadCompanyData: %v", err)
	}
	byName := map[string]companyStarterSkill{}
	for _, sk := range data.StarterSkills {
		byName[sk.Name] = sk
	}
	for _, name := range []string{StarterSkillSecretaryAnalysis, StarterSkillSquadReporting, StarterSkillDirectiveExecution} {
		sk, ok := byName[name]
		if !ok {
			t.Fatalf("starter skill %q is missing from company_template_data.json", name)
		}
		if sk.Description == "" {
			t.Errorf("%s: empty description", name)
		}
		if !strings.HasPrefix(sk.Content, "---\nname: "+name+"\n") {
			t.Errorf("%s: SKILL.md does not start with its frontmatter name", name)
		}
		if len(sk.Files) == 0 || sk.Files[0].Content == "" {
			t.Errorf("%s: no reference file", name)
		}
	}
}

func TestSecretaryDeskTitleIsStable(t *testing.T) {
	t.Parallel()
	// Setup finds the desk by this title, so the title is part of the contract.
	if SecretaryDeskProjectTitle != "Secretary desk" {
		t.Fatalf("SecretaryDeskProjectTitle = %q", SecretaryDeskProjectTitle)
	}
}
