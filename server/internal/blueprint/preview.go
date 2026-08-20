package blueprint

import (
	"fmt"
	"sort"
	"strings"
)

const (
	PreviewActionCreate   = "create"
	PreviewActionReuse    = "reuse"
	PreviewActionConflict = "conflict"

	RuntimeRequirementMatched = "matched"
	RuntimeRequirementMapped  = "mapped"
	RuntimeRequirementMissing = "missing"
	RuntimeRequirementNone    = "none"
)

type Inventory struct {
	Squads          []ExistingResource
	Agents          []ExistingResource
	Skills          []ExistingResource
	Runtimes        []ExistingRuntime
	RuntimeMappings []RuntimeMapping
	ProvidedEnv     []ProvidedEnvVar
}

type ExistingResource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ExistingRuntime struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
}

type RuntimeMapping struct {
	Provider  string `json:"provider"`
	RuntimeID string `json:"runtime_id"`
}

type ProvidedEnvVar struct {
	AgentRef string `json:"agent_ref"`
	Name     string `json:"name"`
}

type Preview struct {
	Summary           PreviewSummary `json:"summary"`
	Squads            []ResourcePlan `json:"squads"`
	Agents            []AgentPlan    `json:"agents"`
	Skills            []ResourcePlan `json:"skills"`
	Errors            []PreviewIssue `json:"errors,omitempty"`
	Warnings          []PreviewIssue `json:"warnings,omitempty"`
	HasBlockingIssues bool           `json:"has_blocking_issues"`
}

type PreviewSummary struct {
	Squads ResourceSummary `json:"squads"`
	Agents ResourceSummary `json:"agents"`
	Skills ResourceSummary `json:"skills"`
}

type ResourceSummary struct {
	Create   int `json:"create"`
	Reuse    int `json:"reuse"`
	Conflict int `json:"conflict"`
}

type ResourcePlan struct {
	Ref        string `json:"ref"`
	Name       string `json:"name"`
	Action     string `json:"action"`
	ExistingID string `json:"existing_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type AgentPlan struct {
	ResourcePlan
	Runtime    RuntimeRequirement `json:"runtime"`
	MissingEnv []string           `json:"missing_env,omitempty"`
}

type RuntimeRequirement struct {
	Provider  string `json:"provider,omitempty"`
	Status    string `json:"status"`
	RuntimeID string `json:"runtime_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type PreviewIssue struct {
	Code    string `json:"code"`
	Ref     string `json:"ref,omitempty"`
	Message string `json:"message"`
}

func PreviewManifest(manifest Manifest, inventory Inventory) (Preview, error) {
	if err := ValidateManifest(manifest); err != nil {
		return Preview{}, err
	}

	squadNameCounts := countNames(manifest.Squads, func(s Squad) string { return s.Name })
	agentNameCounts := countNames(manifest.Agents, func(a Agent) string { return a.Name })
	skillNameCounts := countNames(manifest.Skills, func(s Skill) string { return s.Name })

	existingSquads := resourcesByName(inventory.Squads)
	existingAgents := resourcesByName(inventory.Agents)
	existingSkills := resourcesByName(inventory.Skills)
	runtimesByProvider := runtimesByProvider(inventory.Runtimes)
	runtimesByID := runtimesByID(inventory.Runtimes)
	runtimeMappings := runtimeMappingsByProvider(inventory.RuntimeMappings)
	providedEnv := providedEnvByAgentRef(inventory.ProvidedEnv)

	preview := Preview{
		Squads: make([]ResourcePlan, 0, len(manifest.Squads)),
		Agents: make([]AgentPlan, 0, len(manifest.Agents)),
		Skills: make([]ResourcePlan, 0, len(manifest.Skills)),
	}

	for _, squad := range manifest.Squads {
		plan := planResource(squad.Ref, squad.Name, existingSquads, squadNameCounts)
		preview.Squads = append(preview.Squads, plan)
		preview.Summary.Squads.add(plan.Action)
		if plan.Action == PreviewActionConflict {
			preview.addError("duplicate_squad_name", squad.Ref, plan.Reason)
		}
	}

	for _, skill := range manifest.Skills {
		plan := planResource(skill.Ref, skill.Name, existingSkills, skillNameCounts)
		preview.Skills = append(preview.Skills, plan)
		preview.Summary.Skills.add(plan.Action)
		if plan.Action == PreviewActionConflict {
			preview.addError("duplicate_skill_name", skill.Ref, plan.Reason)
		}
	}

	for _, agent := range manifest.Agents {
		resource := planResource(agent.Ref, agent.Name, existingAgents, agentNameCounts)
		plan := AgentPlan{
			ResourcePlan: resource,
			Runtime:      resolveRuntimeRequirement(agent.Runtime, runtimesByProvider, runtimesByID, runtimeMappings),
			MissingEnv:   missingEnv(agent, providedEnv[agent.Ref]),
		}
		preview.Agents = append(preview.Agents, plan)
		preview.Summary.Agents.add(plan.Action)
		if plan.Action == PreviewActionConflict {
			preview.addError("duplicate_agent_name", agent.Ref, plan.Reason)
		}
		if plan.Runtime.Status == RuntimeRequirementMissing {
			preview.addError("missing_runtime", agent.Ref, plan.Runtime.Reason)
		}
		for _, envName := range plan.MissingEnv {
			preview.addError("missing_env", agent.Ref, fmt.Sprintf("environment variable %q is required for import", envName))
		}
	}

	preview.HasBlockingIssues = len(preview.Errors) > 0
	return preview, nil
}

func planResource(ref, name string, existing map[string]ExistingResource, counts map[string]int) ResourcePlan {
	normalized := normalizeName(name)
	if counts[normalized] > 1 {
		return ResourcePlan{
			Ref:    ref,
			Name:   name,
			Action: PreviewActionConflict,
			Reason: fmt.Sprintf("duplicate name %q in blueprint", name),
		}
	}
	if found, ok := existing[normalized]; ok {
		return ResourcePlan{
			Ref:        ref,
			Name:       name,
			Action:     PreviewActionReuse,
			ExistingID: found.ID,
		}
	}
	return ResourcePlan{
		Ref:    ref,
		Name:   name,
		Action: PreviewActionCreate,
	}
}

func resolveRuntimeRequirement(runtime Runtime, byProvider map[string]ExistingRuntime, byID map[string]ExistingRuntime, mappings map[string]string) RuntimeRequirement {
	provider := strings.TrimSpace(runtime.Provider)
	if provider == "" {
		return RuntimeRequirement{Status: RuntimeRequirementNone}
	}
	if runtimeID := mappings[provider]; runtimeID != "" {
		if _, ok := byID[runtimeID]; ok {
			return RuntimeRequirement{
				Provider:  provider,
				Status:    RuntimeRequirementMapped,
				RuntimeID: runtimeID,
			}
		}
		return RuntimeRequirement{
			Provider: provider,
			Status:   RuntimeRequirementMissing,
			Reason:   fmt.Sprintf("mapped runtime %q was not found in this workspace", runtimeID),
		}
	}
	if found, ok := byProvider[provider]; ok {
		return RuntimeRequirement{
			Provider:  provider,
			Status:    RuntimeRequirementMatched,
			RuntimeID: found.ID,
		}
	}
	return RuntimeRequirement{
		Provider: provider,
		Status:   RuntimeRequirementMissing,
		Reason:   fmt.Sprintf("runtime provider %q is not available in this workspace", provider),
	}
}

func missingEnv(agent Agent, provided map[string]struct{}) []string {
	if len(agent.CustomEnvSchema) == 0 {
		return nil
	}
	missing := make([]string, 0, len(agent.CustomEnvSchema))
	for _, env := range agent.CustomEnvSchema {
		if _, ok := provided[env.Name]; !ok {
			missing = append(missing, env.Name)
		}
	}
	sort.Strings(missing)
	return missing
}

func (p *Preview) addError(code, ref, message string) {
	p.Errors = append(p.Errors, PreviewIssue{
		Code:    code,
		Ref:     ref,
		Message: message,
	})
}

func (s *ResourceSummary) add(action string) {
	switch action {
	case PreviewActionCreate:
		s.Create++
	case PreviewActionReuse:
		s.Reuse++
	case PreviewActionConflict:
		s.Conflict++
	}
}

func countNames[T any](items []T, nameFn func(T) string) map[string]int {
	counts := map[string]int{}
	for _, item := range items {
		counts[normalizeName(nameFn(item))]++
	}
	return counts
}

func resourcesByName(resources []ExistingResource) map[string]ExistingResource {
	out := make(map[string]ExistingResource, len(resources))
	for _, resource := range resources {
		out[normalizeName(resource.Name)] = resource
	}
	return out
}

func runtimesByProvider(runtimes []ExistingRuntime) map[string]ExistingRuntime {
	out := make(map[string]ExistingRuntime, len(runtimes))
	for _, runtime := range runtimes {
		if _, exists := out[runtime.Provider]; !exists {
			out[runtime.Provider] = runtime
		}
	}
	return out
}

func runtimesByID(runtimes []ExistingRuntime) map[string]ExistingRuntime {
	out := make(map[string]ExistingRuntime, len(runtimes))
	for _, runtime := range runtimes {
		out[runtime.ID] = runtime
	}
	return out
}

func runtimeMappingsByProvider(mappings []RuntimeMapping) map[string]string {
	out := make(map[string]string, len(mappings))
	for _, mapping := range mappings {
		out[mapping.Provider] = mapping.RuntimeID
	}
	return out
}

func providedEnvByAgentRef(envVars []ProvidedEnvVar) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	for _, envVar := range envVars {
		if out[envVar.AgentRef] == nil {
			out[envVar.AgentRef] = map[string]struct{}{}
		}
		out[envVar.AgentRef][envVar.Name] = struct{}{}
	}
	return out
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
