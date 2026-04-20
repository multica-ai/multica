package handler

import (
	"encoding/json"
	"net/http"
	"strings"
)

type StructuredTaskSpecResponse struct {
	Goal          string   `json:"goal"`
	Audience      []string `json:"audience"`
	Output        string   `json:"output"`
	Constraints   []string `json:"constraints"`
	Style         []string `json:"style"`
	OpenQuestions []string `json:"open_questions"`
}

type ClarifyStructuredTaskRequest struct {
	OriginalInput string `json:"original_input"`
}

type CheckStructuredTaskClarityRequest struct {
	Goal          string   `json:"goal"`
	Audience      []string `json:"audience"`
	Output        string   `json:"output"`
	Constraints   []string `json:"constraints"`
	Style         []string `json:"style"`
	OpenQuestions []string `json:"open_questions"`
}

type CheckStructuredTaskClarityResponse struct {
	ClarityStatus string   `json:"clarity_status"`
	Reason        []string `json:"reason"`
	Suggestions   []string `json:"suggestions"`
}

var structuredStyleKeywords = []string{
	"formal",
	"professional",
	"concise",
	"friendly",
	"creative",
	"technical",
	"business",
}

func splitStructuredLines(value string) []string {
	parts := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

func splitStructuredTags(value string) []string {
	replacer := strings.NewReplacer(";", ",", "|", ",")
	parts := strings.Split(replacer.Replace(value), ",")
	seen := make(map[string]struct{}, len(parts))
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		tags = append(tags, trimmed)
	}
	return tags
}

func inferStructuredGoal(text string) string {
	replacer := strings.NewReplacer("!", ".", "?", ".", "\n", ".")
	parts := strings.Split(replacer.Replace(text), ".")
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func inferStructuredAudience(text string) []string {
	lowerText := strings.ToLower(text)
	markers := []string{"for ", "audience:", "target:"}
	for _, marker := range markers {
		idx := strings.Index(lowerText, marker)
		if idx < 0 {
			continue
		}
		value := strings.TrimSpace(text[idx+len(marker):])
		if value == "" {
			continue
		}
		for _, sep := range []string{"\n", ".", "!", "?"} {
			if cut := strings.Index(value, sep); cut >= 0 {
				value = value[:cut]
			}
		}
		return splitStructuredTags(value)
	}
	return nil
}

func inferStructuredOutput(lines []string) string {
	for _, line := range lines {
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "output") ||
			strings.Contains(lowerLine, "deliverable") ||
			strings.Contains(lowerLine, "result") {
			return line
		}
	}
	return ""
}

func inferStructuredConstraints(lines []string) []string {
	constraints := make([]string, 0)
	for _, line := range lines {
		lowerLine := strings.ToLower(line)
		if strings.HasPrefix(line, "-") ||
			strings.HasPrefix(line, "*") ||
			strings.Contains(lowerLine, "constraint") ||
			strings.Contains(lowerLine, "must") ||
			strings.Contains(lowerLine, "should") ||
			strings.Contains(lowerLine, "cannot") ||
			strings.Contains(lowerLine, "do not") {
			constraints = append(constraints, strings.TrimSpace(strings.TrimLeft(line, "-* ")))
		}
	}
	return constraints
}

func inferStructuredStyle(text string) []string {
	lowerText := strings.ToLower(text)
	styles := make([]string, 0)
	for _, keyword := range structuredStyleKeywords {
		if strings.Contains(lowerText, keyword) {
			styles = append(styles, keyword)
		}
	}
	return styles
}

func computeStructuredOpenQuestions(goal string, audience []string, output string) []string {
	questions := make([]string, 0, 3)
	if strings.TrimSpace(goal) == "" {
		questions = append(questions, "Need a clear task goal.")
	}
	if strings.TrimSpace(output) == "" {
		questions = append(questions, "Need the expected output format or deliverable.")
	}
	if len(audience) == 0 {
		questions = append(questions, "Audience is not explicit.")
	}
	return questions
}

func buildStructuredTaskSpec(originalInput string) StructuredTaskSpecResponse {
	trimmed := strings.TrimSpace(originalInput)
	lines := splitStructuredLines(trimmed)
	goal := inferStructuredGoal(trimmed)
	audience := inferStructuredAudience(trimmed)
	output := inferStructuredOutput(lines)

	return StructuredTaskSpecResponse{
		Goal:          goal,
		Audience:      audience,
		Output:        output,
		Constraints:   inferStructuredConstraints(lines),
		Style:         inferStructuredStyle(trimmed),
		OpenQuestions: computeStructuredOpenQuestions(goal, audience, output),
	}
}

func (h *Handler) ClarifyStructuredTask(w http.ResponseWriter, r *http.Request) {
	var req ClarifyStructuredTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.OriginalInput) == "" {
		writeError(w, http.StatusBadRequest, "original_input is required")
		return
	}

	writeJSON(w, http.StatusOK, buildStructuredTaskSpec(req.OriginalInput))
}

func (h *Handler) CheckStructuredTaskClarity(w http.ResponseWriter, r *http.Request) {
	var req CheckStructuredTaskClarityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	openQuestions := req.OpenQuestions
	if openQuestions == nil {
		openQuestions = computeStructuredOpenQuestions(req.Goal, req.Audience, req.Output)
	}

	response := CheckStructuredTaskClarityResponse{
		ClarityStatus: "clear",
		Reason:        []string{},
		Suggestions:   []string{},
	}

	if strings.TrimSpace(req.Goal) == "" {
		response.Reason = append(response.Reason, "Goal is missing.")
	}
	if strings.TrimSpace(req.Output) == "" {
		response.Reason = append(response.Reason, "Output is missing.")
	}

	if len(response.Reason) > 0 {
		response.ClarityStatus = "blocked"
		response.Suggestions = append(response.Suggestions, "Fill in Goal and Output before execution.")
		writeJSON(w, http.StatusOK, response)
		return
	}

	if len(openQuestions) > 0 {
		response.ClarityStatus = "risky"
		response.Reason = append(response.Reason, "Some task details still need confirmation.")
		response.Suggestions = append(response.Suggestions, "Review the open questions and refine the structured fields.")
		writeJSON(w, http.StatusOK, response)
		return
	}

	response.Reason = append(response.Reason, "Goal and Output are explicit enough to proceed.")
	writeJSON(w, http.StatusOK, response)
}
