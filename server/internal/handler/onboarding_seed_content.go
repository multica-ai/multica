package handler

import (
	_ "embed"
	"encoding/json"
	"strings"
)

type onboardingSeedContentFile struct {
	InstallTitle          string   `json:"install_title"`
	InstallDescription    []string `json:"install_description"`
	AgentGuideTitle       string   `json:"agent_guide_title"`
	AgentGuideDescription []string `json:"agent_guide_description"`
	FollowupComment       string   `json:"followup_comment"`
}

type onboardingSeedContent struct {
	InstallTitle          string
	InstallDescription    string
	AgentGuideTitle       string
	AgentGuideDescription string
	FollowupComment       string
}

//go:embed onboarding_seed_content.json
var onboardingSeedContentJSON []byte

var onboardingSeedContents = mustLoadOnboardingSeedContents()

func mustLoadOnboardingSeedContents() map[string]onboardingSeedContent {
	var files map[string]onboardingSeedContentFile
	if err := json.Unmarshal(onboardingSeedContentJSON, &files); err != nil {
		panic("decode onboarding seed content: " + err.Error())
	}
	contents := make(map[string]onboardingSeedContent, len(files))
	for locale, file := range files {
		contents[locale] = onboardingSeedContent{
			InstallTitle:          file.InstallTitle,
			InstallDescription:    strings.Join(file.InstallDescription, "\n"),
			AgentGuideTitle:       file.AgentGuideTitle,
			AgentGuideDescription: strings.Join(file.AgentGuideDescription, "\n"),
			FollowupComment:       file.FollowupComment,
		}
	}
	return contents
}

func onboardingSeedContentForLocale(locale string) (onboardingSeedContent, bool) {
	content, ok := onboardingSeedContents[locale]
	return content, ok
}
