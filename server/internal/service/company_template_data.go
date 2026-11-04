package service

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
)

// The starter company's data (spec 3.4): the floors, the squad cards, the
// stage owners and the agent instruction templates. The TypeScript definitions
// in packages/core are the source; company_template_data.json is generated
// from them by `pnpm generate:company-template`, and a core test keeps the two
// equal. Edit the TypeScript, then regenerate.

//go:embed company_template_data.json
var companyTemplateJSON []byte

// Start options, as the wizard sends them (spec 3.4).
const (
	CompanyStartAll         = "all"
	CompanyStartPerFloor    = "per_floor"
	CompanyStartLeavePaused = "leave_paused"
)

// Step statuses. "failed" carries the reason in the step's message.
const (
	CompanyStepCreated = "created"
	CompanyStepReused  = "reused"
	CompanyStepFailed  = "failed"
)

// companyData is the embedded JSON. Field names match the generator's output.
type companyData struct {
	FloorKeys            []string                     `json:"floor_keys"`
	FloorNames           map[string]string            `json:"floor_names"`
	StartOptions         []string                     `json:"start_options"`
	Squads               []companySquad               `json:"squads"`
	StageOwners          []companyStageOwner          `json:"stage_owners"`
	InstructionTemplates []companyInstructionTemplate `json:"instruction_templates"`
	StarterSkills        []companyStarterSkill        `json:"starter_skills"`
}

type companySquad struct {
	Key         string           `json:"key"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Floor       string           `json:"floor"`
	Leader      string           `json:"leader"`
	Roles       []companyRole    `json:"roles"`
	Routines    []companyRoutine `json:"routines"`
	Stages      []string         `json:"stages"`
	Charter     companyCharter   `json:"charter"`
}

type companyRole struct {
	Key           string `json:"key"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	AgentTemplate string `json:"agentTemplate"`
	Runtime       string `json:"runtime"`
	Role          string `json:"role"`
}

type companyRoutine struct {
	Key            string `json:"key"`
	Title          string `json:"title"`
	Cadence        string `json:"cadence"`
	Cron           string `json:"cron"`
	Description    string `json:"description"`
	ActiveAtCreate bool   `json:"activeAtCreate"`
	FloorStart     bool   `json:"floorStart"`
}

type companyCharter struct {
	Mission          string                  `json:"mission"`
	Responsibilities []companyResponsibility `json:"responsibilities"`
	Outputs          []companyOutput         `json:"outputs"`
	Boundaries       string                  `json:"boundaries"`
	Escalation       string                  `json:"escalation"`
	Intake           string                  `json:"intake"`
}

type companyResponsibility struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

type companyOutput struct {
	Key         string `json:"key"`
	Kind        string `json:"kind"`
	CadenceHint string `json:"cadenceHint"`
}

type companyStageOwner struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Owner string `json:"owner"`
}

type companyInstructionTemplate struct {
	ID           string `json:"id"`
	Instructions string `json:"instructions"`
}

// companyStarterSkill is a skill the starter company installs: Content is the
// SKILL.md with its frontmatter, Files the reference files beside it.
type companyStarterSkill struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Content     string               `json:"content"`
	Files       []companyStarterFile `json:"files"`
}

type companyStarterFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

var loadCompanyData = sync.OnceValues(func() (companyData, error) {
	var data companyData
	if err := json.Unmarshal(companyTemplateJSON, &data); err != nil {
		return companyData{}, fmt.Errorf("parse company template data: %w", err)
	}
	return data, nil
})

// squad returns the card for a squad key.
func (d companyData) squad(key string) (companySquad, bool) {
	for _, s := range d.Squads {
		if s.Key == key {
			return s, true
		}
	}
	return companySquad{}, false
}

// instructions returns the instruction text of a template id.
func (d companyData) instructions(id string) (string, bool) {
	for _, t := range d.InstructionTemplates {
		if t.ID == id {
			return t.Instructions, true
		}
	}
	return "", false
}

// routineWanted reports whether a start option leaves a routine running. "all"
// runs every routine of the chosen squads; "per_floor" runs the routines created
// active and each floor's lead routine; "leave_paused" runs only the routines
// created active (the Secretary's brief and the market scan).
func routineWanted(start string, r companyRoutine) bool {
	switch start {
	case CompanyStartAll:
		return true
	case CompanyStartPerFloor:
		return r.ActiveAtCreate || r.FloorStart
	default:
		return r.ActiveAtCreate
	}
}

// operatingReviewRoutine is the review a squad's charter creates. A card that
// lists the review sets its start flags; a card that does not gets the defaults
// of a paused review, which the charter path makes for every squad.
func operatingReviewRoutine(s companySquad) companyRoutine {
	for _, r := range s.Routines {
		if r.Title == OperatingReviewTitle {
			return r
		}
	}
	return companyRoutine{Key: "operating_review", Title: OperatingReviewTitle, Cadence: "weekly"}
}

// companySquadKeys validates a request's squad keys: known, unique, at least one.
func companySquadKeys(d companyData, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("choose at least one squad")
	}
	known := make(map[string]bool, len(d.Squads))
	for _, s := range d.Squads {
		known[s.Key] = true
	}
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if !known[k] {
			return nil, fmt.Errorf("unknown squad %q", k)
		}
		if seen[k] {
			return nil, fmt.Errorf("squad %q is listed twice", k)
		}
		seen[k] = true
	}
	return keys, nil
}

// selectedSquads returns the cards of the chosen squads in the order of the
// starter company, so the apply runs floors first and leaders before members.
func selectedSquads(d companyData, keys []string) []companySquad {
	chosen := make(map[string]bool, len(keys))
	for _, k := range keys {
		chosen[k] = true
	}
	var out []companySquad
	for _, s := range d.Squads {
		if chosen[s.Key] {
			out = append(out, s)
		}
	}
	return out
}

// validCompanyStart reports whether a start option is one the apply knows.
func validCompanyStart(d companyData, start string) bool {
	return slices.Contains(d.StartOptions, start)
}
